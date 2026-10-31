package report

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type received struct {
	payload map[string]any
	files   map[string]string
}

func fakeWebhook(t *testing.T, status int) (*httptest.Server, *[]received) {
	t.Helper()
	var got []received
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			t.Errorf("not multipart: %v", err)
		}
		rec := received{files: map[string]string{}}
		json.Unmarshal([]byte(r.FormValue("payload_json")), &rec.payload)
		for field, hs := range r.MultipartForm.File {
			f, _ := hs[0].Open()
			b, _ := io.ReadAll(f)
			f.Close()
			rec.files[field+":"+hs[0].Filename] = string(b)
		}
		got = append(got, rec)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

// A short report is one message with no files; nothing in it can mention
// anybody.
func TestAShortReportIsOneMessage(t *testing.T) {
	srv, got := fakeWebhook(t, http.StatusNoContent)
	err := Send(context.Background(), srv.Client(), srv.URL, Report{
		What: "The comic reader shows a blank page @everyone", Version: "v0.9.99", Platform: "windows/amd64",
		Client: "desktop", Account: "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 || len((*got)[0].files) != 0 {
		t.Fatalf("got %+v", *got)
	}
	p := (*got)[0].payload
	content := p["content"].(string)
	for _, want := range []string{"**What happened**", "blank page", "LANcast v0.9.99 · windows/amd64 · desktop · admin"} {
		if !strings.Contains(content, want) {
			t.Errorf("content lacks %q:\n%s", want, content)
		}
	}
	if am := p["allowed_mentions"].(map[string]any); len(am["parse"].([]any)) != 0 {
		t.Errorf("mentions allowed: %v", am)
	}
}

// A long report is cut to fit Discord's message and attached whole, and a log
// rides as its own file.
func TestALongReportAndALogAreAttached(t *testing.T) {
	srv, got := fakeWebhook(t, http.StatusOK)
	long := strings.Repeat("it broke again. ", 300)
	err := Send(context.Background(), srv.Client(), srv.URL, Report{What: long, Log: []byte("line 1\nline 2\n")})
	if err != nil {
		t.Fatal(err)
	}
	r := (*got)[0]
	if c := r.payload["content"].(string); len([]rune(c)) > 2000 || !strings.HasSuffix(c, "(full text attached)") {
		t.Errorf("content is %d runes", len([]rune(c)))
	}
	if !strings.Contains(r.files["files[0]:report.txt"], strings.TrimSpace(long)) {
		t.Error("the full report is not attached")
	}
	if r.files["files[1]:lancastd-tail.log"] != "line 1\nline 2\n" {
		t.Errorf("files: %v", r.files)
	}
}

func TestARefusalIsAnError(t *testing.T) {
	srv, _ := fakeWebhook(t, http.StatusNotFound)
	if err := Send(context.Background(), srv.Client(), srv.URL, Report{What: "x"}); err == nil {
		t.Fatal("a 404 from the webhook was taken as sent")
	}
	if err := Send(context.Background(), srv.Client(), "", Report{What: "x"}); err != ErrNoWebhook {
		t.Fatalf("no URL: %v", err)
	}
}
