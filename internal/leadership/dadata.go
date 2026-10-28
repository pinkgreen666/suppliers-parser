package leadership

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const Endpoint = "https://suggestions.dadata.ru/suggestions/api/4_1/rs/findById/party"

type APIError struct {
	Status     int
	RetryAfter string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("DaData HTTP %d (Retry-After: %s)", e.Status, e.RetryAfter)
}
func (e *APIError) Stop() bool { return e.Status == 401 || e.Status == 403 || e.Status == 429 }

type DaData struct {
	token    string
	client   *http.Client
	endpoint string
}

func NewDaData(token string, timeout time.Duration) (*DaData, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("задайте DADATA_API_KEY в окружении")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("таймаут должен быть положительным")
	}
	return &DaData{token: strings.TrimSpace(token), client: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, endpoint: Endpoint}, nil
}

type fio struct{ Surname, Name, Patronymic string }

func (f fio) Full() string {
	return strings.Join(strings.Fields(f.Surname+" "+f.Name+" "+f.Patronymic), " ")
}

type manager struct {
	Type, Name, Post, INN, OGRN string
	FIO                         fio
	Invalidity                  json.RawMessage
}
type party struct {
	INN, OGRN, Type string
	BranchType      string `json:"branch_type"`
	Name            struct {
		Full string `json:"full_with_opf"`
	}
	FIO        fio
	Management *struct{ Name, Post string }
	Managers   []manager
	State      struct {
		ActualityDate *int64 `json:"actuality_date"`
	}
}

func (d *DaData) Lookup(ctx context.Context, id Identifier) (Result, error) {
	payload, _ := json.Marshal(map[string]any{"query": id.Value, "type": id.EntityType, "branch_type": "MAIN", "count": 10})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Token "+d.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("DaData request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, &APIError{resp.StatusCode, resp.Header.Get("Retry-After")}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil {
		return Result{}, fmt.Errorf("DaData read: %w", err)
	}
	if len(body) > 2*1024*1024 {
		return Result{}, fmt.Errorf("ответ DaData слишком большой")
	}
	var answer struct{ Suggestions []struct{ Data party } }
	if err := json.Unmarshal(body, &answer); err != nil {
		return Result{}, fmt.Errorf("некорректный JSON DaData")
	}
	// A JSON object without suggestions is a protocol error, not a negative lookup.
	var shape map[string]json.RawMessage
	if err := json.Unmarshal(body, &shape); err != nil || shape["suggestions"] == nil || string(shape["suggestions"]) == "null" {
		return Result{}, fmt.Errorf("в ответе DaData нет массива suggestions")
	}
	result := Result{Status: "not_found", Source: "dadata", CheckedAt: time.Now().UTC(), Leaders: []Leader{}}
	var selected []party
	for _, suggestion := range answer.Suggestions {
		p := suggestion.Data
		exact := id.Kind == "inn" && p.INN == id.Value || id.Kind == "ogrn" && p.OGRN == id.Value
		if exact && p.Type == id.EntityType && (p.BranchType == "MAIN" || p.Type == "INDIVIDUAL" && p.BranchType == "") {
			selected = append(selected, p)
		}
	}
	if len(selected) == 0 {
		if len(answer.Suggestions) > 0 {
			result.Status = "needs_review"
			result.Note = "ответ не содержит точного совпадения реквизитов головной организации"
		}
		return result, nil
	}
	if len(selected) != 1 {
		result.Status = "needs_review"
		result.Note = "несколько точных совпадений"
		return result, nil
	}
	p := selected[0]
	result.INN = p.INN
	result.OGRN = p.OGRN
	result.EntityType = p.Type
	result.CompanyName = p.Name.Full
	if p.State.ActualityDate != nil {
		t := time.UnixMilli(*p.State.ActualityDate).UTC()
		result.RegistryUpdatedAt = &t
	}
	if p.Type == "INDIVIDUAL" {
		if name := p.FIO.Full(); name != "" {
			result.Leaders = append(result.Leaders, Leader{Name: name, Type: "INDIVIDUAL", Post: "Индивидуальный предприниматель"})
		}
	} else if len(p.Managers) > 0 {
		for _, m := range p.Managers {
			leader := Leader{Post: strings.TrimSpace(m.Post), INN: m.INN, OGRN: m.OGRN}
			switch m.Type {
			case "EMPLOYEE":
				leader.Type = "PERSON"
				leader.Name = m.FIO.Full()
			case "LEGAL":
				leader.Type = "LEGAL"
				leader.Name = strings.TrimSpace(m.Name)
			default:
				result.Status = "needs_review"
				result.Note = "неизвестный тип руководителя"
				continue
			}
			if len(m.Invalidity) > 0 && string(m.Invalidity) != "null" {
				result.Status = "needs_review"
				result.Note = "есть отметка о недостоверности сведений руководителя"
			}
			if leader.Name != "" {
				result.Leaders = append(result.Leaders, leader)
			} else {
				result.Status = "needs_review"
				result.Note = "руководитель без имени"
			}
		}
	} else if p.Management != nil && strings.TrimSpace(p.Management.Name) != "" {
		leader := Leader{Name: strings.TrimSpace(p.Management.Name), Post: strings.TrimSpace(p.Management.Post), Type: "PERSON"}
		if leader.Post == "" {
			leader.Type = "UNKNOWN"
			result.Status = "needs_review"
			result.Note = "базовый ответ не позволяет подтвердить тип руководителя; возможно управляющая организация"
		}
		result.Leaders = append(result.Leaders, leader)
	}
	if result.Status != "needs_review" {
		result.Status = "no_management"
		if len(result.Leaders) > 0 {
			result.Status = "found"
		}
	}
	return result, nil
}
