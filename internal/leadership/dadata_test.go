package leadership

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestIdentifierValidation(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"7707083893", true}, {"7719402047", true}, {"784806113663", true}, {"7707083894", false}, {"784806113664", false}, {"0000000000", false}, {"77O7083893", false},
	} {
		if ValidINN(tc.value) != tc.valid {
			t.Errorf("INN %s", tc.value)
		}
	}
	for _, tc := range []struct {
		value string
		valid bool
	}{{"1157746078984", true}, {"1157746078985", false}, {"0000000000000", false}} {
		if ValidOGRN(tc.value) != tc.valid {
			t.Errorf("OGRN %s", tc.value)
		}
	}
	id, err := Identify(Company{INN: "bad", OGRN: "1157746078984"})
	if err != nil || id.Kind != "ogrn" {
		t.Fatalf("fallback: %+v %v", id, err)
	}
}

func TestDaDataOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, body, status, kind string
		leaders                  int
	}{
		{"director", `{"suggestions":[{"data":{"inn":"7719402047","ogrn":"1157746078984","type":"LEGAL","branch_type":"MAIN","management":{"name":"Иванов Иван Иванович","post":"ДИРЕКТОР"}}}]}`, "found", "PERSON", 1},
		{"individual", `{"suggestions":[{"data":{"inn":"784806113663","type":"INDIVIDUAL","branch_type":"MAIN","fio":{"surname":"Иванов","name":"Иван"}}}]}`, "found", "INDIVIDUAL", 1},
		{"managing legal entity", `{"suggestions":[{"data":{"inn":"7719402047","type":"LEGAL","branch_type":"MAIN","managers":[{"type":"LEGAL","name":"ООО Управление","inn":"7707083893"}]}}]}`, "found", "LEGAL", 1},
		{"multiple directors", `{"suggestions":[{"data":{"inn":"7719402047","type":"LEGAL","branch_type":"MAIN","managers":[{"type":"EMPLOYEE","fio":{"surname":"Иванов","name":"Иван"},"post":"ДИРЕКТОР"},{"type":"EMPLOYEE","fio":{"surname":"Петров","name":"Петр"},"post":"ДИРЕКТОР"}]}}]}`, "found", "PERSON", 2},
		{"unknown manager", `{"suggestions":[{"data":{"inn":"7719402047","type":"LEGAL","branch_type":"MAIN","management":{"name":"ООО Управление"}}}]}`, "needs_review", "UNKNOWN", 1},
		{"invalid manager", `{"suggestions":[{"data":{"inn":"7719402047","type":"LEGAL","branch_type":"MAIN","managers":[{"type":"EMPLOYEE","fio":{"surname":"Иванов","name":"Иван"},"invalidity":{"code":"FTS"}}]}}]}`, "needs_review", "PERSON", 1},
		{"no management", `{"suggestions":[{"data":{"inn":"7719402047","type":"LEGAL","branch_type":"MAIN"}}]}`, "no_management", "", 0},
		{"ogrn", `{"suggestions":[{"data":{"inn":"7719402047","ogrn":"1157746078984","type":"LEGAL","branch_type":"MAIN","management":{"name":"Иванов Иван","post":"ДИРЕКТОР"}}}]}`, "found", "PERSON", 1},
		{"individual no branch", `{"suggestions":[{"data":{"inn":"784806113663","type":"INDIVIDUAL","fio":{"surname":"Иванов","name":"Иван"}}}]}`, "found", "INDIVIDUAL", 1},
		{"not found", `{"suggestions":[]}`, "not_found", "", 0},
		{"wrong company", `{"suggestions":[{"data":{"inn":"7707083893","type":"LEGAL","branch_type":"MAIN"}}]}`, "needs_review", "", 0},
		{"branch", `{"suggestions":[{"data":{"inn":"7719402047","type":"LEGAL","branch_type":"BRANCH"}}]}`, "needs_review", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := Identifier{"inn", "7719402047", "LEGAL"}
			if strings.HasPrefix(tc.name, "individual") {
				id = Identifier{"inn", "784806113663", "INDIVIDUAL"}
			}
			if tc.name == "ogrn" {
				id = Identifier{"ogrn", "1157746078984", "LEGAL"}
			}
			d, _ := NewDaData("test-token", time.Second)
			d.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Token test-token" {
					t.Error("missing authorization")
				}
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				if request["query"] != id.Value || request["branch_type"] != "MAIN" || request["type"] != id.EntityType {
					t.Errorf("request=%v", request)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			result, err := d.Lookup(context.Background(), id)
			if err != nil || result.Status != tc.status || len(result.Leaders) != tc.leaders {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if tc.leaders > 0 && result.Leaders[0].Type != tc.kind {
				t.Errorf("leader=%+v", result.Leaders[0])
			}
		})
	}
}

func TestDaDataErrors(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		d, _ := NewDaData("secret", time.Second)
		d.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"60"}}, Body: io.NopCloser(strings.NewReader("sensitive response"))}, nil
		})
		_, err := d.Lookup(context.Background(), Identifier{"inn", "7719402047", "LEGAL"})
		var api *APIError
		if !errors.As(err, &api) || api.Stop() != (status != 500) || strings.Contains(err.Error(), "sensitive") {
			t.Fatalf("error=%v", err)
		}
	}
	for _, body := range []string{`{}`, `{"suggestions":null}`, `broken`} {
		d, _ := NewDaData("secret", time.Second)
		d.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		if _, err := d.Lookup(context.Background(), Identifier{"inn", "7719402047", "LEGAL"}); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
