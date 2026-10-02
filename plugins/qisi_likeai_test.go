package plugins_test

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQisiLikeAITaskContract(t *testing.T) {
	source, err := os.ReadFile("../qisi/likeai.plugin.js")
	require.NoError(t, err)
	registry := jsplugin.NewRegistry()
	plugin, err := registry.Register(string(source), jsplugin.Options{})
	require.NoError(t, err)
	require.NoError(t, jsplugin.ValidateV1Meta(plugin.Meta))
	_, found := registry.Generation().LookupDeclaredRoute("POST", "/likeai/task/create_task")
	require.True(t, found)
	call := func(name string, args ...any) map[string]any {
		t.Helper()
		value, err := plugin.Engine.Call(t.Context(), name, args...)
		require.NoError(t, err)
		encoded, err := common.Marshal(value)
		require.NoError(t, err)
		var result map[string]any
		require.NoError(t, common.Unmarshal(encoded, &result))
		return result
	}
	request := map[string]any{"api_name": "doubao_seedance_2_5", "prompt": "test", "duration": 5, "resolution": "720p", "kwargs": map[string]any{"generate_audio": false}}
	ctx := map[string]any{"model": "doubao_seedance_2_5", "requestBody": request, "apiKey": "fixture-key", "baseUrl": "https://task.likeai.pro/task-api"}
	t.Run("fixed upstream and explicit false", func(t *testing.T) {
		result := call("buildSubmitRequest", ctx)
		assert.Equal(t, "https://task.likeai.pro/task-api/task/create_task", result["url"])
		assert.Equal(t, "fixture-key", result["headers"].(map[string]any)["X-API-Key"])
		body := result["body"].(map[string]any)
		assert.Equal(t, false, body["kwargs"].(map[string]any)["generate_audio"])
		assert.Equal(t, float64(5), call("extractUsage", ctx)["seconds"])
	})
	t.Run("reject unsafe billing inputs before submission", func(t *testing.T) {
		for _, duration := range []any{-1, 0, 3, 31, 1e20, 4.5, "5"} {
			_, err := plugin.Engine.Call(t.Context(), "extractUsage", map[string]any{"requestBody": map[string]any{"api_name": "doubao_seedance_2_5", "prompt": "test", "duration": duration}})
			require.Error(t, err, "duration=%v", duration)
		}
		for _, patch := range []map[string]any{{"n": 128}, {"api_name": "unknown"}, {"resolution": "1080p"}, {"kwargs": map[string]any{"video_urls": []string{"https://example.com/video"}}}} {
			body := map[string]any{"api_name": "doubao_seedance_2_5", "prompt": "test"}
			for key, value := range patch {
				body[key] = value
			}
			_, err := plugin.Engine.Call(t.Context(), "extractUsage", map[string]any{"requestBody": body})
			require.Error(t, err)
		}
		bad := map[string]any{"model": ctx["model"], "requestBody": request, "apiKey": "fixture-key", "baseUrl": "https://other.example"}
		_, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", bad)
		require.Error(t, err)
	})
	t.Run("acceptance state does not store prompt or keys", func(t *testing.T) {
		result := call("parseSubmitResponse", ctx, map[string]any{"body": map[string]any{"code": 200, "data": map[string]any{"task_id": "upstream-123"}}})
		state := result["state"].(map[string]any)["requested"].(map[string]any)
		assert.Equal(t, map[string]any{"api_name": "doubao_seedance_2_5", "resolution": "720p", "duration": float64(5)}, state)
		_, err := plugin.Engine.Call(t.Context(), "parseSubmitResponse", ctx, map[string]any{"body": map[string]any{"code": 500}})
		require.Error(t, err)
	})
	t.Run("qisiTV empty image kwargs are accepted without exposing extra controls", func(t *testing.T) {
		imageRequest := map[string]any{"api_name": "doubao_seedream_4_5", "prompt": "test", "kwargs": map[string]any{}}
		usage := call("extractUsage", map[string]any{"requestBody": imageRequest})
		assert.Equal(t, float64(1), usage["image_count"])
		imageRequest["kwargs"] = map[string]any{"generate_audio": true}
		_, err := plugin.Engine.Call(t.Context(), "extractUsage", map[string]any{"requestBody": imageRequest})
		require.Error(t, err)
	})
	t.Run("artifact download is bound to persisted media and has no credentials", func(t *testing.T) {
		data := map[string]any{"data": map[string]any{"result": map[string]any{"images": []string{"file:///private", "https://cdn.example/a.png"}, "videos": []string{"https://cdn.example/a.mp4"}}}}
		artifactCtx := map[string]any{"artifactKey": "images.0", "data": data, "apiKey": "fixture-secret", "authHeader": "Bearer fixture-secret", "clientRequest": map[string]any{"method": "GET", "headers": map[string]any{"Range": "bytes=0-99", "Authorization": "Bearer client-secret"}}}
		descriptor := call("buildContentRequest", artifactCtx)
		assert.Equal(t, map[string]any{"url": "https://cdn.example/a.png", "method": "GET", "credentialless": true}, descriptor)
		artifactCtx["artifactKey"] = "videos.0"
		artifactCtx["clientRequest"] = map[string]any{"method": "HEAD"}
		assert.Equal(t, "HEAD", call("buildContentRequest", artifactCtx)["method"])
		for _, key := range []string{"images.1", "images.00", "images.-1", "images.0/../../", "https://other.example/"} {
			artifactCtx["artifactKey"] = key
			_, err := plugin.Engine.Call(t.Context(), "buildContentRequest", artifactCtx)
			require.Error(t, err, key)
		}
		artifactCtx["artifactKey"] = "images.0"
		artifactCtx["clientRequest"] = map[string]any{"method": "POST"}
		_, err := plugin.Engine.Call(t.Context(), "buildContentRequest", artifactCtx)
		require.Error(t, err)
		for _, status := range []string{"SUCCESS", "IN_PROGRESS"} {
			value, err := plugin.Engine.Call(t.Context(), "listArtifacts", map[string]any{"status": status, "data": data})
			require.NoError(t, err)
			encoded, err := common.Marshal(value)
			require.NoError(t, err)
			if status == "SUCCESS" {
				assert.JSONEq(t, `[{"key":"images.0","type":"image"},{"key":"videos.0","type":"video"}]`, string(encoded))
			} else {
				assert.JSONEq(t, `[]`, string(encoded))
			}
		}
	})
	t.Run("unknown poll results must not refund or finish tasks", func(t *testing.T) {
		for _, body := range []map[string]any{{"code": 500}, {"code": 200, "data": map[string]any{"status": "completed"}}, {"code": 200, "data": map[string]any{"status": "future_state"}}} {
			assert.Equal(t, "UNKNOWN", call("parseTaskResult", ctx, body)["status"])
		}
	})
	t.Run("image settlement counts media and ignores oversized results", func(t *testing.T) {
		imageCtx := map[string]any{"state": map[string]any{"requested": map[string]any{"api_name": "doubao_seedream_4_5", "resolution": "1440p"}}}
		body := map[string]any{"code": 200, "data": map[string]any{"status": "completed", "result": map[string]any{"images": []string{"https://cdn.example/a.png"}, "videos": []string{}}}}
		assert.Equal(t, float64(1), call("extractUsageOnComplete", imageCtx, nil, body)["image_count"])
		assert.Equal(t, "SUCCESS", call("parseTaskResult", imageCtx, body)["status"])
		oversized := make([]string, 129)
		for i := range oversized {
			oversized[i] = "https://cdn.example/a.png"
		}
		body["data"].(map[string]any)["result"] = map[string]any{"images": oversized}
		assert.Empty(t, call("extractUsageOnComplete", imageCtx, nil, body))
	})
	t.Run("public task response hides upstream identity", func(t *testing.T) {
		value, err := plugin.Engine.CallPath(t.Context(), "native", []string{"query"}, ctx, map[string]any{"task_id": "public-123", "status": "SUCCESS", "data": map[string]any{"data": map[string]any{"task_id": "private-upstream", "result": map[string]any{"images": []string{"https://cdn.example/image.png", "file:///private"}, "secret": "hidden"}}}})
		require.NoError(t, err)
		encoded, err := common.Marshal(value)
		require.NoError(t, err)
		assert.Contains(t, string(encoded), "public-123")
		assert.Contains(t, string(encoded), "https://cdn.example/image.png")
		assert.NotContains(t, string(encoded), "private-upstream")
		assert.NotContains(t, string(encoded), "file://")
		assert.NotContains(t, string(encoded), "hidden")
	})
}
