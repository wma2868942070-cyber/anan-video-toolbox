package leonardo

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestGraphQLNullDataDoesNotExposeUsers(t *testing.T) {
	resp := map[string]any{
		"data": nil,
		"errors": []any{
			map[string]any{"message": "invalid token"},
		},
	}

	data, _ := resp["data"].(map[string]any)
	users, _ := data["users"].([]any)
	if len(users) != 0 {
		t.Fatalf("users = %v, want empty", users)
	}
	if got := GraphQLErrorMessage(resp); got != "invalid token" {
		t.Fatalf("GraphQLErrorMessage = %q", got)
	}
}

func TestUserIDFromTokenAcceptsJSONStringHasuraClaims(t *testing.T) {
	claims, err := json.Marshal(map[string]any{
		"https://hasura.io/jwt/claims": `{"x-hasura-user-id":"account-123","x-hasura-default-role":"user"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." +
		base64.RawURLEncoding.EncodeToString(claims) + ".signature"
	if got := UserIDFromToken(token); got != "account-123" {
		t.Fatalf("UserIDFromToken = %q, want account-123", got)
	}
}

func TestWaitForGenerationCompletionAcceptsImagesWhileStatusPending(t *testing.T) {
	imageCalls := 0
	result := waitForGenerationCompletion(
		100*time.Millisecond,
		time.Millisecond,
		func() (string, error) { return "PENDING", nil },
		func() ([]string, error) {
			imageCalls++
			if imageCalls < 2 {
				return nil, nil
			}
			return []string{"https://cdn.example/generated.png"}, nil
		},
	)
	if !result.Success {
		t.Fatalf("result = %+v, want success", result)
	}
	if len(result.Images) != 1 {
		t.Fatalf("images = %v, want one image", result.Images)
	}
}

func TestWaitForGenerationCompletionDoesNotReturnEmptyCompletedResult(t *testing.T) {
	imageCalls := 0
	result := waitForGenerationCompletion(
		100*time.Millisecond,
		time.Millisecond,
		func() (string, error) { return "COMPLETED", nil },
		func() ([]string, error) {
			imageCalls++
			if imageCalls < 3 {
				return nil, nil
			}
			return []string{"https://cdn.example/generated.png"}, nil
		},
	)
	if !result.Success || len(result.Images) != 1 {
		t.Fatalf("result = %+v, want delayed image success", result)
	}
}

func TestWaitForGenerationCompletionStopsOnFailure(t *testing.T) {
	result := waitForGenerationCompletion(
		time.Second,
		time.Millisecond,
		func() (string, error) { return "FAILED", nil },
		func() ([]string, error) { return nil, nil },
	)
	if result.Success || result.Error != "generation failed" {
		t.Fatalf("result = %+v, want generation failed", result)
	}
}

func TestDecodeImageDataURL(t *testing.T) {
	body, contentType, err := decodeImageDataURL("data:image/png;base64,aGVsbG8=")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" || contentType != "image/png" {
		t.Fatalf("decoded data = %q contentType=%q", body, contentType)
	}
}

func TestSafeResourceLabelRedactsInlineImageBytes(t *testing.T) {
	got := safeResourceLabel("data:image/png;base64,very-secret-image-bytes")
	if got != "data:image/png;base64,<inline image>" {
		t.Fatalf("safeResourceLabel = %q", got)
	}
}
