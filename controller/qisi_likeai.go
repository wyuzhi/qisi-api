package controller

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	_ "golang.org/x/image/webp"
)

const qisiLikeAIPlugin = "qisi-likeai"
const qisiLikeAIBaseURL = "https://task.likeai.pro/task-api"
const qisiLikeAIMaxUpload = 4 << 20

var qisiLikeAIUploadClient = &http.Client{
	Timeout:       45 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
}

type qisiLikeAIModel struct {
	APIName string `json:"api_name"`
	Name    string `json:"name"`
	Type    string `json:"type"`
}

var qisiLikeAIModels = []qisiLikeAIModel{
	{APIName: "doubao_seedream_4_5", Name: "Seedream 4.5", Type: "image"},
	{APIName: "doubao_seedance_2_5", Name: "Seedance 2.5", Type: "video"},
}

// QisiLikeAIBearerOnly prevents alternate provider or URL credentials from
// reaching the shared token middleware on this public browser API.
func QisiLikeAIBearerOnly(c *gin.Context) {
	parts := strings.Fields(c.GetHeader("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		qisiLikeAIError(c, http.StatusUnauthorized, "A qisi API Bearer key is required")
		c.Abort()
		return
	}
	c.Request.Header.Set("Authorization", "Bearer "+parts[1])
	c.Request.Header.Del("Sec-WebSocket-Protocol")
	c.Header("Cache-Control", "private, no-store")
}

func qisiLikeAIError(c *gin.Context, status int, message string) {
	c.Header("Cache-Control", "private, no-store")
	c.JSON(status, gin.H{"code": status, "error": message})
}

// qisiLikeAIChannels uses the same group, token and provider pricing boundaries
// as task submission. No upstream call is needed to reveal this safe catalogue.
func qisiLikeAIChannels(c *gin.Context) ([]qisiLikeAIModel, []*model.Channel, error) {
	items := make([]qisiLikeAIModel, 0, len(qisiLikeAIModels))
	channels := make([]*model.Channel, 0, len(qisiLikeAIModels))
	plugin, enabled := jsplugin.DefaultRegistry.Get(qisiLikeAIPlugin)
	if !enabled {
		return items, channels, nil
	}
	groups, err := getModelListGroups(c)
	if err != nil {
		return nil, nil, err
	}
	limitEnabled := common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled)
	limitValue, _ := common.GetContextKey(c, constant.ContextKeyTokenModelLimit)
	limits, _ := limitValue.(map[string]bool)
	for _, item := range qisiLikeAIModels {
		if !slices.Contains(plugin.Meta.Models, item.APIName) || (limitEnabled &&
			!limits[item.APIName] && !limits[ratio_setting.RoutingMatchModelName(item.APIName)]) {
			continue
		}
		expression, configured := billing_setting.ResolveTaskBillingExpr(qisiLikeAIPlugin, item.APIName, item.APIName)
		schema, _ := plugin.Meta.UsageForModel(item.APIName)
		if !configured || !billing_setting.TaskExprCompatible(expression, schema) {
			continue
		}
		for _, group := range groups.ownerGroups {
			channel, channelErr := model.GetChannel(group, item.APIName, 0, []dto.ChannelFilter{{Kind: dto.FilterTaskPluginIdentity, TaskPluginKey: qisiLikeAIPlugin}})
			if channelErr != nil {
				return nil, nil, channelErr
			}
			if channel == nil || channel.Type != constant.ChannelTypeTaskPlugin || channel.Status != common.ChannelStatusEnabled ||
				strings.TrimRight(channel.GetBaseURL(), "/") != qisiLikeAIBaseURL || strings.TrimSpace(channel.Key) == "" {
				continue
			}
			items = append(items, item)
			channels = append(channels, channel)
			break
		}
	}
	return items, channels, nil
}

func QisiLikeAIListModels(c *gin.Context) {
	items, _, err := qisiLikeAIChannels(c)
	if err != nil {
		qisiLikeAIError(c, http.StatusInternalServerError, "Unable to load available models")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"models": items, "total": len(items)}, "error": ""})
}

func QisiLikeAIUpload(c *gin.Context) {
	quota, err := model.GetUserQuota(c.GetInt("id"), true)
	if err != nil {
		qisiLikeAIError(c, http.StatusInternalServerError, "Unable to check account balance")
		return
	}
	if quota <= 0 {
		qisiLikeAIError(c, http.StatusPaymentRequired, "Add balance before uploading reference images")
		return
	}
	_, channels, err := qisiLikeAIChannels(c)
	if err != nil || len(channels) == 0 {
		qisiLikeAIError(c, http.StatusServiceUnavailable, "No configured LikeAI channel is available for this key")
		return
	}
	key, _, keyErr := channels[0].GetNextEnabledKey()
	if keyErr != nil || strings.TrimSpace(key) == "" {
		qisiLikeAIError(c, http.StatusServiceUnavailable, "LikeAI upload is unavailable")
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, qisiLikeAIMaxUpload+64<<10)
	if err := c.Request.ParseMultipartForm(qisiLikeAIMaxUpload + 64<<10); err != nil {
		var limitErr *http.MaxBytesError
		if errors.As(err, &limitErr) {
			qisiLikeAIError(c, http.StatusRequestEntityTooLarge, "Reference images must be at most 4 MiB")
		} else {
			qisiLikeAIError(c, http.StatusBadRequest, "Upload one image using multipart field file")
		}
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	form := c.Request.MultipartForm
	files := form.File["file"]
	if len(files) != 1 || len(form.File) != 1 || len(form.Value) != 0 {
		qisiLikeAIError(c, http.StatusBadRequest, "Upload exactly one image using multipart field file")
		return
	}
	if files[0].Size <= 0 || files[0].Size > qisiLikeAIMaxUpload {
		qisiLikeAIError(c, http.StatusRequestEntityTooLarge, "Reference images must be nonempty and at most 4 MiB")
		return
	}
	file, err := files[0].Open()
	if err != nil {
		qisiLikeAIError(c, http.StatusBadRequest, "Unable to read reference image")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, qisiLikeAIMaxUpload+1))
	if err != nil || len(data) > qisiLikeAIMaxUpload {
		qisiLikeAIError(c, http.StatusRequestEntityTooLarge, "Unable to read image within upload limit")
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	ext := map[string]string{"jpeg": "jpg", "png": "png", "webp": "webp"}[format]
	if err != nil || ext == "" || config.Width <= 0 || config.Height <= 0 || int64(config.Width) > 64_000_000/int64(config.Height) {
		qisiLikeAIError(c, http.StatusUnsupportedMediaType, "Reference must be a valid JPEG, PNG or WebP image up to 64 megapixels")
		return
	}
	contentType := "image/" + format
	filename := "reference." + ext
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": filename}))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		qisiLikeAIError(c, http.StatusInternalServerError, "Unable to prepare reference image")
		return
	}
	_, _ = part.Write(data)
	_ = writer.Close()
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, qisiLikeAIBaseURL+"/files", &body)
	if err != nil {
		qisiLikeAIError(c, http.StatusInternalServerError, "Unable to prepare upload")
		return
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-API-Key", key)
	response, err := qisiLikeAIUploadClient.Do(req)
	if err != nil {
		qisiLikeAIError(c, http.StatusBadGateway, "Reference upload failed; try again later")
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		qisiLikeAIError(c, http.StatusBadGateway, "LikeAI rejected the reference upload")
		return
	}
	responseData, err := io.ReadAll(io.LimitReader(response.Body, 64<<10+1))
	var result struct {
		ID        string `json:"id"`
		URL       string `json:"url"`
		CreatedAt int64  `json:"created_at"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err != nil || len(responseData) > 64<<10 || common.Unmarshal(responseData, &result) != nil {
		qisiLikeAIError(c, http.StatusBadGateway, "LikeAI returned an invalid upload response")
		return
	}
	location, err := url.Parse(result.URL)
	if err != nil || location.Scheme != "https" || location.Host != "tos.likeai.pro" || location.User != nil ||
		location.Fragment != "" || !strings.HasPrefix(location.Path, "/files/") || len(result.URL) > 4096 ||
		result.ID == "" || len(result.ID) > 128 || strings.ContainsAny(result.ID, "\r\n") {
		qisiLikeAIError(c, http.StatusBadGateway, "LikeAI returned an invalid reference URL")
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": result.ID, "object": "file", "url": result.URL,
		"bytes": len(data), "filename": filename, "content_type": contentType,
		"created_at": result.CreatedAt, "expires_at": result.ExpiresAt})
}

func QisiLikeAIArtifact(c *gin.Context) {
	kind := c.Param("kind")
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index < 0 || index >= 64 || !slices.Contains([]string{"images", "videos", "covers", "audios"}, kind) {
		qisiLikeAIError(c, http.StatusNotFound, "Task or artifact not found")
		return
	}
	task, exists, err := model.GetByTaskId(c.GetInt("id"), c.Param("task_id"))
	if err != nil {
		qisiLikeAIError(c, http.StatusInternalServerError, "Unable to load task")
		return
	}
	if !exists || task == nil || task.Platform != constant.TaskPlatform(qisiLikeAIPlugin) || !taskHasPluginExecution(task) ||
		task.PrivateData.Execution.TaskPlugin.Key != qisiLikeAIPlugin || !task.ResultRetrievable() {
		qisiLikeAIError(c, http.StatusNotFound, "Task or artifact not found")
		return
	}
	if task.Status != model.TaskStatusSuccess {
		qisiLikeAIError(c, http.StatusConflict, "Task artifacts are not ready")
		return
	}
	c.Params = append(c.Params, gin.Param{Key: "key", Value: task.TaskID}, gin.Param{Key: "artifact_key", Value: kind + "." + strconv.Itoa(index)})
	TaskArtifactContent(c)
}
