package radar

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestDates(t *testing.T) {
	a := time.Date(2024, 5, 20, 23, 30, 0, 0, time.UTC)
	if got := Dates(a, a.Add(2*time.Hour)); !reflect.DeepEqual(got, []string{"2024/05/20", "2024/05/21"}) {
		t.Fatal(got)
	}
}
func TestDiscoveryPaginationAndFiltering(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("prefix") != "2024/05/20/KFTG/" {
			t.Error(r.URL)
		}
		if r.URL.Query().Get("continuation-token") == "" {
			fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>a+b</NextContinuationToken><Contents><Key>2024/05/20/KFTG/KFTG20240520_000059_V06</Key><Size>100</Size></Contents><Contents><Key>2024/05/20/KFTG/KFTG20240520_000059_V06_MDM</Key><Size>100</Size></Contents><Contents><Key>2024/05/20/KFTG/KFTG20240520_000528_V06</Key><Size>0</Size></Contents></ListBucketResult>`)
		} else {
			if r.URL.Query().Get("continuation-token") != "a+b" {
				t.Error("token")
			}
			fmt.Fprint(w, `<ListBucketResult><Contents><Key>2024/05/20/KFTG/KFTG20240520_000944_V06</Key><Size>100</Size></Contents><Contents><Key>2024/05/20/KABC/KABC20240520_000944_V06</Key><Size>100</Size></Contents></ListBucketResult>`)
		}
	}))
	defer srv.Close()
	from := time.Date(2024, 5, 20, 0, 0, 0, 0, time.UTC)
	objects, e := (Source{srv.URL, srv.Client(), 1000}).Discover(context.Background(), "KFTG", from, from.Add(time.Hour))
	if e != nil || len(objects) != 2 || calls != 2 {
		t.Fatalf("%v %v %d", objects, e, calls)
	}
	if !objects[0].Time.After(objects[1].Time) {
		t.Fatal("not newest first")
	}
}
func TestParse(t *testing.T) {
	for _, key := range []string{"2024/05/20/KFTG/KFTG20240520_000059_V06_MDM", "2024/05/20/KFTG/readme", "2024/05/19/KFTG/KFTG20240520_000059_V06"} {
		if _, ok := Parse(key, "KFTG"); ok {
			t.Fatal(key)
		}
	}
}
