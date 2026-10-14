package radar

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Object struct {
	Key  string
	Size int64
	ETag string
	Time time.Time
}
type Source struct {
	BaseURL string
	Client  *http.Client
	MaxSize int64
}

var volumeName = regexp.MustCompile(`^([A-Z][A-Z0-9]{3})([0-9]{8}_[0-9]{6})(?:_V0[6-9])?(?:\.gz)?$`)

func Dates(from, to time.Time) []string {
	out := []string{}
	d := from.UTC()
	d = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	for !d.After(to.UTC()) {
		out = append(out, d.Format("2006/01/02"))
		d = d.AddDate(0, 0, 1)
	}
	return out
}
func Parse(key, station string) (time.Time, bool) {
	parts := strings.Split(key, "/")
	if len(parts) != 5 || parts[3] != station {
		return time.Time{}, false
	}
	m := volumeName.FindStringSubmatch(parts[4])
	if m == nil || m[1] != station {
		return time.Time{}, false
	}
	t, e := time.Parse("20060102_150405", m[2])
	return t, e == nil && strings.Join(parts[:3], "/") == t.Format("2006/01/02")
}
func (s Source) Discover(ctx context.Context, station string, from, to time.Time) ([]Object, error) {
	out := []Object{}
	for _, date := range Dates(from, to) {
		token := ""
		seen := map[string]bool{}
		for {
			q := url.Values{"list-type": {"2"}, "prefix": {date + "/" + station + "/"}, "max-keys": {"1000"}}
			if token != "" {
				q.Set("continuation-token", token)
			}
			req, e := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/?"+q.Encode(), nil)
			if e != nil {
				return nil, e
			}
			res, e := s.Client.Do(req)
			if e != nil {
				return nil, e
			}
			var page struct {
				Contents []struct {
					Key  string
					Size int64
					ETag string
				}
				IsTruncated           bool
				NextContinuationToken string
			}
			if res.StatusCode != 200 {
				res.Body.Close()
				return nil, fmt.Errorf("source listing HTTP %d", res.StatusCode)
			}
			e = xml.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&page)
			res.Body.Close()
			if e != nil {
				return nil, e
			}
			for _, o := range page.Contents {
				t, ok := Parse(o.Key, station)
				if ok && o.Size > 0 && o.Size <= s.MaxSize && !t.Before(from) && !t.After(to) {
					out = append(out, Object{o.Key, o.Size, o.ETag, t})
				}
			}
			if !page.IsTruncated {
				break
			}
			token = page.NextContinuationToken
			if token == "" || seen[token] {
				return nil, fmt.Errorf("invalid repeated pagination token")
			}
			seen[token] = true
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out, nil
}
