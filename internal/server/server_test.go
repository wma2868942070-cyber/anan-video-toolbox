package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/config"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/leonardo"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/service"
	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Bootstrap(""); err != nil {
		t.Fatal(err)
	}
	if err := st.AddModel("测试模型", "11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatal(err)
	}
	models, err := st.ListModels()
	if err != nil || len(models) == 0 {
		t.Fatalf("list models: %v", err)
	}
	if err := st.SetDefaultModel(models[0].ID); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{APIKey: "test-key", CORSOrigins: "*", Port: "8001"}
	svc := service.NewLeonardoPool(st, leonardo.New())
	return New(cfg, st, svc), st
}

func TestBundledCanvasCanUseMarkerKeyOnlyFromTrustedLocalPage(t *testing.T) {
	srv, _ := newTestServer(t)

	trusted := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	trusted.RemoteAddr = "127.0.0.1:32100"
	trusted.Header.Set("Authorization", "Bearer local-anan-video-toolbox")
	trusted.Header.Set("Referer", "http://127.0.0.1:8001/infinite-canvas/")
	trustedRes := httptest.NewRecorder()
	srv.engine.ServeHTTP(trustedRes, trusted)
	if trustedRes.Code != http.StatusOK {
		t.Fatalf("trusted canvas status = %d body=%s", trustedRes.Code, trustedRes.Body.String())
	}

	untrusted := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	untrusted.RemoteAddr = "127.0.0.1:32101"
	untrusted.Header.Set("Authorization", "Bearer local-anan-video-toolbox")
	untrustedRes := httptest.NewRecorder()
	srv.engine.ServeHTTP(untrustedRes, untrusted)
	if untrustedRes.Code != http.StatusUnauthorized {
		t.Fatalf("untrusted marker-key status = %d", untrustedRes.Code)
	}
}

func TestVideoTaskAcceptsInfiniteCanvasMultipartForm(t *testing.T) {
	srv, _ := newTestServer(t)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for key, value := range map[string]string{
		"model":           "hailuo-03",
		"prompt":          "测试视频",
		"seconds":         "6",
		"size":            "1280x720",
		"resolution_name": "720p",
	} {
		if err := form.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/videos", &body)
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", form.FormDataContentType())
	res := httptest.NewRecorder()
	srv.engine.ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("multipart video status = %d body=%s", res.Code, res.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("missing video task id")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		statusReq := httptest.NewRequest(http.MethodGet, "/v1/videos/"+created.ID, nil)
		statusReq.Header.Set("Authorization", "Bearer test-key")
		statusRes := httptest.NewRecorder()
		srv.engine.ServeHTTP(statusRes, statusReq)
		if bytes.Contains(statusRes.Body.Bytes(), []byte(`"status":"failed"`)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("multipart video task did not reach failed state without accounts")
}

func TestReferenceImageExtensionUsesBytesBeforeFilename(t *testing.T) {
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01}
	ext, err := referenceImageExtension("generated-node.png", "image/png", jpeg)
	if err != nil {
		t.Fatal(err)
	}
	if ext != "jpg" {
		t.Fatalf("extension = %q, want jpg", ext)
	}
}

func TestImageEditRouteAcceptsInfiniteCanvasMultipartForm(t *testing.T) {
	srv, _ := newTestServer(t)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("model", "anan-default"); err != nil {
		t.Fatal(err)
	}
	if err := form.WriteField("prompt", "保持主体并调整颜色"); err != nil {
		t.Fatal(err)
	}
	file, err := form.CreateFormFile("image", "reference.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("\x89PNG\r\n\x1a\n")); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", form.FormDataContentType())
	res := httptest.NewRecorder()
	srv.engine.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("image edit status = %d body=%s", res.Code, res.Body.String())
	}
	if !bytes.Contains(res.Body.Bytes(), []byte("账号池中没有可用账号")) {
		t.Fatalf("unexpected image edit body=%s", res.Body.String())
	}
}

func TestModelsEndpointAndAuth(t *testing.T) {
	srv, _ := newTestServer(t)

	unauthorized := httptest.NewRecorder()
	srv.engine.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	srv.engine.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("models status = %d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		Object string `json:"object"`
		Data   []struct {
			ID             string `json:"id"`
			Type           string `json:"type"`
			AudioPolicy    string `json:"audio_policy"`
			CreditCostMode string `json:"credit_cost_mode"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Object != "list" || len(payload.Data) < 2 || payload.Data[0].ID != "anan-default" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	foundLTX := false
	for _, model := range payload.Data {
		if model.ID == "ltx-2.3-fast" {
			foundLTX = true
			if model.Type != "video" || model.AudioPolicy != "optional" || model.CreditCostMode != "dynamic" {
				t.Fatalf("unexpected LTX metadata: %+v", model)
			}
		}
	}
	if !foundLTX {
		t.Fatal("ltx-2.3-fast missing from /v1/models")
	}
}

func TestCORSPreflightDoesNotRequireAPIKey(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodOptions, "/v1/images/generations", nil)
	req.Header.Set("Origin", "https://canvas.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Private-Network", "true")
	res := httptest.NewRecorder()
	srv.engine.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d", res.Code)
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := res.Header().Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Fatalf("allow private network = %q", got)
	}
}

func TestDefaultModelAliases(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, alias := range []string{"", "anan-default", "leostudio-default", "gpt-image-1", "dall-e-3", "测试模型"} {
		modelID, _, _, err := srv.resolveGenerationRequest(openAIImageRequest{Model: alias})
		if err != nil {
			t.Fatalf("alias %q: %v", alias, err)
		}
		if modelID != "11111111-1111-1111-1111-111111111111" {
			t.Fatalf("alias %q resolved to %q", alias, modelID)
		}
	}
}

func TestLeonardoAdminIsLoopbackOnlyAndRedacted(t *testing.T) {
	srv, st := newTestServer(t)
	secretCookie := "__Secure-better-auth.session_token=secret-cookie-value"
	if err := st.AddCookie(secretCookie); err != nil {
		t.Fatal(err)
	}
	row, err := st.GetCookieByValue(secretCookie)
	if err != nil || row == nil {
		t.Fatalf("get seeded cookie: %v", err)
	}
	if err := st.UpdateCookieProfile(row.ID, "stable-account-secret", "private@example.test", 42); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateCookieSessionSuccess(row.ID, "secret.jwt.token", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkCookieError(row.ID, "private upstream error secret-value"); err != nil {
		t.Fatal(err)
	}

	local := httptest.NewRequest(http.MethodGet, "/admin", nil)
	local.RemoteAddr = "127.0.0.1:32100"
	localRes := httptest.NewRecorder()
	srv.engine.ServeHTTP(localRes, local)
	if localRes.Code != http.StatusOK {
		t.Fatalf("local admin status = %d body=%s", localRes.Code, localRes.Body.String())
	}
	body := localRes.Body.String()
	for _, secret := range []string{secretCookie, "secret-cookie-value", "stable-account-secret", "private@example.test", "secret.jwt.token", "secret-value"} {
		if strings.Contains(body, secret) {
			t.Fatalf("admin response leaked %q", secret)
		}
	}

	remote := httptest.NewRequest(http.MethodGet, "/admin", nil)
	remote.RemoteAddr = "198.51.100.20:32101"
	remoteRes := httptest.NewRecorder()
	srv.engine.ServeHTTP(remoteRes, remote)
	if remoteRes.Code != http.StatusForbidden {
		t.Fatalf("remote admin status = %d, want 403", remoteRes.Code)
	}

	for _, path := range []string{"/admin/local/cookies", "/admin/cookies"} {
		res := httptest.NewRecorder()
		srv.engine.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		if res.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, res.Code)
		}
	}
}
