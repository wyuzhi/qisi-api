package controller

import (
	"bytes"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type qisiUploadTransport func(*http.Request) (*http.Response, error)

func (f qisiUploadTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func setupQisiLikeAITest(t *testing.T) *model.Task {
	t.Helper()
	previousDB := model.DB
	initModelListColumnNames(t)
	model.DB = previousDB
	task := setupGenericTaskTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Ability{}))
	previousRegistry := jsplugin.DefaultRegistry
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	source, err := os.ReadFile("../qisi/likeai.plugin.js")
	require.NoError(t, err)
	_, err = jsplugin.DefaultRegistry.Register(string(source), jsplugin.Options{})
	require.NoError(t, err)
	settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	previousSettings := *settings
	settings.PluginBillingExpr = map[string]string{
		"qisi-likeai::doubao_seedream_4_5": `tier("image", u("image_count") * 0.1)`,
		"qisi-likeai::doubao_seedance_2_5": `tier("video", u("seconds") * 0.1)`,
	}
	previousClient := qisiLikeAIUploadClient
	previousMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		jsplugin.DefaultRegistry = previousRegistry
		*settings = previousSettings
		qisiLikeAIUploadClient = previousClient
		common.MemoryCacheEnabled = previousMemoryCache
	})
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", task.UserId).Update("quota", 100).Error)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", task.ChannelId).Updates(map[string]any{
		"type": constant.ChannelTypeTaskPlugin, "key": "private-provider-key", "base_url": qisiLikeAIBaseURL,
		"setting": `{"task_plugin_key":"qisi-likeai"}`,
	}).Error)
	for _, item := range qisiLikeAIModels {
		require.NoError(t, model.DB.Create(&model.Ability{Group: "default", Model: item.APIName, ChannelId: task.ChannelId, Enabled: true}).Error)
	}
	task.Platform = qisiLikeAIPlugin
	task.PrivateData.Execution = &model.TaskExecutionSnapshot{TaskPlugin: &model.TaskPluginSnapshot{Key: qisiLikeAIPlugin}}
	require.NoError(t, model.DB.Save(task).Error)
	return task
}

func qisiLikeAIContext(method, path string, body io.Reader) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", 7)
	c.Set("token_id", 42)
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	c.Request = httptest.NewRequest(method, path, body)
	c.Request.Header.Set("Authorization", "Bearer sk-client-key")
	return c, recorder
}

func TestQisiLikeAIModelCatalogueHonorsProviderPricingAndAccess(t *testing.T) {
	task := setupQisiLikeAITest(t)
	tests := []struct {
		name   string
		change func(*gin.Context)
		want   int
	}{
		{name: "configured", want: 2},
		{name: "token model limits", want: 1, change: func(c *gin.Context) {
			common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
			common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"doubao_seedream_4_5": true})
		}},
		{name: "empty token limits deny all", want: 0, change: func(c *gin.Context) { common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true) }},
		{name: "other group", want: 0, change: func(c *gin.Context) { common.SetContextKey(c, constant.ContextKeyTokenGroup, "other") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, recorder := qisiLikeAIContext(http.MethodGet, "/likeai/task/models", nil)
			if test.change != nil {
				test.change(c)
			}
			QisiLikeAIListModels(c)
			assert.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Code int
				Data struct {
					Models []qisiLikeAIModel
					Total  int
				}
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Equal(t, 200, response.Code)
			assert.Len(t, response.Data.Models, test.want)
			assert.Equal(t, test.want, response.Data.Total)
			assert.NotContains(t, recorder.Body.String(), "private-provider-key")
		})
	}
	settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	settings.PluginBillingExpr["qisi-likeai::doubao_seedance_2_5"] = `u("undeclared") * 0.1`
	delete(settings.PluginBillingExpr, "qisi-likeai::doubao_seedream_4_5")
	c, recorder := qisiLikeAIContext(http.MethodGet, "/likeai/task/models", nil)
	QisiLikeAIListModels(c)
	assert.Contains(t, recorder.Body.String(), `"models":[]`)
	settings.PluginBillingExpr["qisi-likeai::doubao_seedance_2_5"] = `u("seconds") * 0.1`
	for _, change := range []map[string]any{{"status": common.ChannelStatusManuallyDisabled}, {"status": common.ChannelStatusEnabled, "setting": `{"task_plugin_key":"other"}`}, {"setting": `{"task_plugin_key":"qisi-likeai"}`, "base_url": "https://attacker.invalid"}} {
		require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", task.ChannelId).Updates(change).Error)
		c, recorder = qisiLikeAIContext(http.MethodGet, "/likeai/task/models", nil)
		QisiLikeAIListModels(c)
		assert.Contains(t, recorder.Body.String(), `"models":[]`)
	}
}

func TestQisiLikeAIUploadValidationAndCredentialSeparation(t *testing.T) {
	setupQisiLikeAITest(t)
	var pngBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	requests := 0
	qisiLikeAIUploadClient = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: qisiUploadTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		assert.Equal(t, qisiLikeAIBaseURL+"/files", r.URL.String())
		assert.Equal(t, "private-provider-key", r.Header.Get("X-API-Key"))
		assert.Empty(t, r.Header.Get("Authorization"))
		assert.Empty(t, r.Header.Get("Cookie"))
		require.NoError(t, r.ParseMultipartForm(5<<20))
		require.Len(t, r.MultipartForm.File["file"], 1)
		assert.Equal(t, "reference.png", r.MultipartForm.File["file"][0].Filename)
		assert.Equal(t, "image/png", r.MultipartForm.File["file"][0].Header.Get("Content-Type"))
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"file-result","url":"https://tos.likeai.pro/files/image.png","created_at":10,"expires_at":20,"private_key":"do-not-expose"}`))}, nil
	})}
	for _, test := range []struct {
		name   string
		data   []byte
		copies int
		status int
	}{
		{"valid PNG with hostile filename", pngBytes.Bytes(), 1, 200},
		{"HTML disguised as PNG", []byte("<html><script>bad()</script></html>"), 1, 415},
		{"over limit", make([]byte, qisiLikeAIMaxUpload+1), 1, 413},
		{"two files", pngBytes.Bytes(), 2, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			for range test.copies {
				part, err := writer.CreateFormFile("file", "../../unsafe.html")
				require.NoError(t, err)
				_, err = part.Write(test.data)
				require.NoError(t, err)
			}
			require.NoError(t, writer.Close())
			c, recorder := qisiLikeAIContext(http.MethodPost, "/likeai/files", &body)
			c.Request.Header.Set("Content-Type", writer.FormDataContentType())
			c.Request.Header.Set("Cookie", "private-browser-session")
			QisiLikeAIUpload(c)
			assert.Equal(t, test.status, recorder.Code)
			assert.NotContains(t, recorder.Body.String(), "do-not-expose")
			if test.status == 200 {
				assert.Contains(t, recorder.Body.String(), `"filename":"reference.png"`)
				assert.NotContains(t, recorder.Body.String(), `"data"`)
			}
		})
	}
	assert.Equal(t, 1, requests)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 7).Update("quota", 0).Error)
	c, recorder := qisiLikeAIContext(http.MethodPost, "/likeai/files", nil)
	QisiLikeAIUpload(c)
	assert.Equal(t, http.StatusPaymentRequired, recorder.Code)
	assert.Equal(t, 1, requests)
}

func TestQisiLikeAIArtifactEnforcesOwnerPluginStatusAndBounds(t *testing.T) {
	task := setupQisiLikeAITest(t)
	for _, test := range []struct {
		name        string
		user        int
		kind, index string
		status      int
	}{
		{"foreign admin token", 8, "images", "0", 404},
		{"invalid kind", 7, "files", "0", 404},
		{"negative index", 7, "images", "-1", 404},
		{"oversized index", 7, "images", "64", 404},
		{"missing artifact", 7, "images", "0", 404},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, recorder := qisiLikeAIContext(http.MethodGet, "/likeai/task/artifact/task_generic/images/0", nil)
			c.Set("id", test.user)
			c.Set("role", common.RoleRootUser)
			c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}, {Key: "kind", Value: test.kind}, {Key: "index", Value: test.index}}
			QisiLikeAIArtifact(c)
			assert.Equal(t, test.status, recorder.Code)
		})
	}
	for _, test := range []struct {
		plugin string
		state  model.TaskStatus
		status int
	}{{"other", model.TaskStatusSuccess, 404}, {qisiLikeAIPlugin, model.TaskStatusInProgress, 409}} {
		task.PrivateData.Execution.TaskPlugin.Key = test.plugin
		task.Status = test.state
		require.NoError(t, model.DB.Save(task).Error)
		c, recorder := qisiLikeAIContext(http.MethodGet, "/likeai/task/artifact/task_generic/images/0", nil)
		c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}, {Key: "kind", Value: "images"}, {Key: "index", Value: "0"}}
		QisiLikeAIArtifact(c)
		assert.Equal(t, test.status, recorder.Code)
	}
}

func TestQisiLikeAIRejectsAlternativeCredentials(t *testing.T) {
	for _, value := range []string{"", "sk-raw", "Basic abc", "Bearer "} {
		c, recorder := qisiLikeAIContext(http.MethodGet, "/likeai/task/models?key=ignored", nil)
		c.Request.Header.Set("Authorization", value)
		c.Request.Header.Set("X-API-Key", "ignored")
		QisiLikeAIBearerOnly(c)
		assert.Equal(t, http.StatusUnauthorized, recorder.Code)
		assert.True(t, c.IsAborted())
	}
}

func TestQisiLikeAIUploadRejectsUnsafeUpstreamResponses(t *testing.T) {
	setupQisiLikeAITest(t)
	var pngBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	for _, test := range []struct {
		name, response string
		upstreamStatus int
	}{
		{"redirect", "", http.StatusTemporaryRedirect},
		{"private URL", `{"id":"file-1","url":"https://127.0.0.1/files/a.png"}`, 200},
		{"foreign host", `{"id":"file-1","url":"https://attacker.invalid/files/a.png"}`, 200},
		{"embedded credentials", `{"id":"file-1","url":"https://secret@tos.likeai.pro/files/a.png"}`, 200},
		{"upstream error body is not exposed", `{"error":"private-provider-key"}`, 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			qisiLikeAIUploadClient = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: qisiUploadTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				assert.Equal(t, "task.likeai.pro", r.URL.Host)
				return &http.Response{StatusCode: test.upstreamStatus, Header: http.Header{"Location": {"https://attacker.invalid"}}, Body: io.NopCloser(strings.NewReader(test.response))}, nil
			})}
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("file", "image.png")
			require.NoError(t, err)
			_, err = part.Write(pngBytes.Bytes())
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			c, recorder := qisiLikeAIContext(http.MethodPost, "/likeai/files", &body)
			c.Request.Header.Set("Content-Type", writer.FormDataContentType())
			QisiLikeAIUpload(c)
			assert.Equal(t, http.StatusBadGateway, recorder.Code)
			assert.Equal(t, 1, calls)
			assert.NotContains(t, recorder.Body.String(), "private-provider-key")
		})
	}
}

func TestQisiLikeAIArtifactUsesProtectedCredentiallessPipeline(t *testing.T) {
	task := setupQisiLikeAITest(t)
	previousFetch := *system_setting.GetFetchSetting()
	system_setting.GetFetchSetting().EnableSSRFProtection = true
	system_setting.GetFetchSetting().AllowPrivateIp = false
	system_setting.GetFetchSetting().AllowedPorts = []string{"443"}
	service.InitHttpClient()
	client := service.GetSSRFProtectedHTTPClient()
	previousTransport := client.Transport
	t.Cleanup(func() { *system_setting.GetFetchSetting() = previousFetch; client.Transport = previousTransport })
	calls := 0
	client.Transport = qisiUploadTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		assert.Equal(t, "https://8.8.8.8/result.png", r.URL.String())
		assert.Empty(t, r.Header.Get("Authorization"))
		assert.Empty(t, r.Header.Get("X-API-Key"))
		assert.Empty(t, r.Header.Get("Cookie"))
		assert.Equal(t, "bytes=0-3", r.Header.Get("Range"))
		return &http.Response{StatusCode: http.StatusPartialContent, Header: http.Header{"Content-Type": {"image/png"}, "Content-Range": {"bytes 0-3/4"}}, Body: io.NopCloser(strings.NewReader("data"))}, nil
	})
	for _, location := range []string{"https://8.8.8.8/result.png", "https://127.0.0.1/result.png"} {
		task.SetData(map[string]any{"code": 200, "data": map[string]any{"result": map[string]any{"images": []string{location}}}})
		require.NoError(t, model.DB.Save(task).Error)
		c, recorder := qisiLikeAIContext(http.MethodGet, "/likeai/task/artifact/task_generic/images/0", nil)
		c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}, {Key: "kind", Value: "images"}, {Key: "index", Value: "0"}}
		c.Request.Header.Set("Range", "bytes=0-3")
		c.Request.Header.Set("Cookie", "browser-private")
		QisiLikeAIArtifact(c)
		if strings.Contains(location, "127.0.0.1") {
			assert.Equal(t, http.StatusBadGateway, recorder.Code)
		} else {
			assert.Equal(t, http.StatusPartialContent, recorder.Code)
			assert.Equal(t, "data", recorder.Body.String())
		}
	}
	assert.Equal(t, 1, calls, "private destination must be rejected before transport")
}
