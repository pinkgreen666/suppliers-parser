package yopt

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductPagination(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.RawQuery != "" {
					if fail {
						http.Error(w, "failed", 500)
						return
					}
					fmt.Fprint(w, `<div class="view-content"><div class="views-row"><div class="title-stroka"><div class="title"><a href="https://yopt.org/product/two">Two</a></div></div></div></div>`)
					return
				}
				fmt.Fprint(w, `<div class="view-content"><div class="views-row"><div class="title-stroka"><div class="title"><a href="https://yopt.org/product/one">One</a></div></div></div></div><ul><li class="pager-next"><a href="?page=1">Next</a></li></ul>`)
			}))
			defer server.Close()
			y := &Yopt{client: server.Client()}
			links, err := y.parseProductLinks(context.Background(), server.URL+"/products")
			if fail {
				if err == nil || links != nil {
					t.Fatalf("partial result: %v %v", links, err)
				}
			} else if err != nil || len(links) != 2 || !strings.HasSuffix(links[1], "/two") {
				t.Fatalf("%v %v", links, err)
			}
		})
	}
}

func TestProductPaginationCycle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<li class="pager-next"><a href="?page=1">Next</a></li>`)
	}))
	defer server.Close()
	y := &Yopt{client: server.Client()}
	links, err := y.parseProductLinks(context.Background(), server.URL+"/products")
	if err == nil || links != nil {
		t.Fatalf("cycle returned partial result: %v %v", links, err)
	}
}
func TestProductPaginationDeduplicatesLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<div class="view-content"><div class="views-row"><div class="title-stroka"><div class="title"><a href="https://yopt.org/products/one">One</a></div></div></div><div class="views-row"><div class="title-stroka"><div class="title"><a href="https://yopt.org/products/one">One</a></div></div></div></div>`)
	}))
	defer server.Close()
	y := &Yopt{client: server.Client()}
	links, err := y.parseProductLinks(context.Background(), server.URL+"/products")
	if err != nil || len(links) != 1 {
		t.Fatalf("%v %v", links, err)
	}
}
