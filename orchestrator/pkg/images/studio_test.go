package images

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSceneFollowsJobsAndFetchesImage(t *testing.T) {
	polls := 0
	var rendered map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/studio/infer":
			if r.Header.Get("X-Job-Id") == "" {
				t.Error("missing X-Job-Id")
			}
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"job":"x","pending":true}`))
		case r.URL.Path[:9] == "/api/job/" && polls == 0:
			polls++
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"pending":true}`))
		case r.URL.Path[:9] == "/api/job/" && polls == 1:
			polls++
			w.Write([]byte(`{"images":[{"tags":"forest, ruins","style":"anime","view":"auto","look":""}]}`))
		case r.URL.Path == "/api/studio/render":
			_ = json.NewDecoder(r.Body).Decode(&rendered)
			w.Write([]byte(`{"url":"/images/abc.png"}`))
		case r.URL.Path == "/images/abc.png":
			w.Write([]byte("PNGDATA"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(srv.URL)
	c.poll = time.Millisecond

	img, err := c.Scene(context.Background(), "森の遺跡")
	if err != nil {
		t.Fatal(err)
	}
	if string(img.Data) != "PNGDATA" || img.Ext != ".png" {
		t.Errorf("img = %+v", img)
	}
	if rendered["tags"] != "forest, ruins" || rendered["style"] != "anime" {
		t.Errorf("render body = %v", rendered)
	}
}

func TestSceneReportsStudioError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error":"ComfyUI に接続できません"}`))
	}))
	defer srv.Close()
	_, err := New(srv.URL).Scene(context.Background(), "x")
	if err == nil || err.Error() != "構図を決められませんでした: ComfyUI に接続できません" {
		t.Fatalf("err = %v", err)
	}
}

func TestNewEmptyDisables(t *testing.T) {
	if New("") != nil {
		t.Error("empty URL should disable images")
	}
}
