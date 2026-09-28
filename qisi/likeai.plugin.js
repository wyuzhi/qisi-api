// qisi API LikeAI adapter. No model calls are made by installing this plugin.
const IMAGE = 'doubao_seedream_4_5';
const VIDEO = 'doubao_seedance_2_5';
// Mirrors the host dto.MaxImageN; oversized results keep reserved usage.
const MAX_IMAGE_COUNT = 128;
const STATUS = { queued: 'QUEUED', pending: 'QUEUED', running: 'IN_PROGRESS', completed: 'SUCCESS', failed: 'FAILURE' };
export const meta = {
  apiVersion: 1, key: 'qisi-likeai', name: 'LikeAI · qisi API', version: '1.0.1',
  author: { name: 'qisi' }, icon: 'text:起司',
  description: { en: 'LikeAI image and video generation', zh: 'LikeAI 图片与视频生成' },
  website: 'https://cheeser.link', baseUrl: 'https://task.likeai.pro/task-api',
  models: [IMAGE, VIDEO], fetchMode: 'per_task', auth: 'api_key',
  routes: [
    { method: 'POST', path: '/likeai/task/create_task', type: 'submit', decode: 'create', render: 'created' },
    { method: 'GET', path: '/likeai/task/query_task/:task_id', type: 'query', render: 'query' },
  ],
  usageSchema: {
    seconds: { type: 'number', unit: 'second', description: { en: 'Video generation unit price', zh: '视频生成单价' } },
    resolution: { enum: ['480p', '720p'], description: { en: 'Output resolution', zh: '输出分辨率' } },
  },
  usageProfiles: [{ models: [IMAGE], schema: {
    image_count: { type: 'number', unit: 'count', unitLabel: { en: 'image', zh: '张' }, description: { en: 'Image generation unit price', zh: '图片生成单价' } },
    resolution: { enum: ['1080p', '1440p', '2160p'], description: { en: 'Output resolution', zh: '输出分辨率' } },
  } }],
};

function object(value, label) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(label + ' must be an object');
  return value;
}
function mediaUrl(value) {
  // Only pass HTTPS references; do not fetch user-supplied URLs in the gateway.
  if (typeof value !== 'string' || value.length > 4096 || !/^https:\/\/[^\s/@]+(?:\/[^\s]*)?$/.test(value))
    throw new Error('Reference images must use HTTPS URLs');
  return value;
}
function normalize(input) {
  const req = object(input, 'Request');
  const model = req.model || req.api_name;
  if (![IMAGE, VIDEO].includes(model)) throw new Error('Unsupported model');
  if (req.model && req.api_name && req.model !== req.api_name) throw new Error('model and api_name must match');
  const allowed = ['model', 'api_name', 'prompt', 'resolution', 'duration', 'aspect_ratio', 'image_urls', 'first_image_url', 'last_image_url', 'kwargs'];
  for (const key of Object.keys(req)) if (!allowed.includes(key)) throw new Error('Unsupported parameter: ' + key);
  if (typeof req.prompt !== 'string' || !req.prompt.trim() || req.prompt.length > 20000) throw new Error('prompt must contain 1 to 20000 characters');
  const resolution = req.resolution || (model === IMAGE ? '1440p' : '720p');
  const resolutions = model === IMAGE ? ['1080p', '1440p', '2160p'] : ['480p', '720p'];
  if (!resolutions.includes(resolution)) throw new Error('Unsupported resolution');
  const out = { api_name: model, prompt: req.prompt, resolution, aspect_ratio: req.aspect_ratio || (model === IMAGE ? '1:1' : 'adaptive') };
  if (!['adaptive', '21:9', '16:9', '9:16', '4:3', '3:4', '1:1', '3:2', '2:3'].includes(out.aspect_ratio)) throw new Error('Unsupported aspect_ratio');
  if (model === VIDEO) {
    const duration = req.duration === undefined ? 5 : req.duration;
    // Automatic duration and video/audio inputs are intentionally not exposed:
    // their final billable input/output duration is not in the public task schema.
    if (!Number.isInteger(duration) || duration < 4 || duration > 30) throw new Error('duration must be an integer from 4 to 30');
    out.duration = duration;
    if (!['adaptive', '21:9', '16:9', '9:16', '4:3', '3:4', '1:1'].includes(out.aspect_ratio)) throw new Error('Unsupported video aspect_ratio');
  } else if (req.duration !== undefined || req.first_image_url !== undefined || req.last_image_url !== undefined) throw new Error('Video parameters cannot be used with an image model');
  if (req.image_urls !== undefined) {
    if (!Array.isArray(req.image_urls) || req.image_urls.length > (model === VIDEO ? 30 : 10)) throw new Error('Too many reference images');
    out.image_urls = req.image_urls.map(mediaUrl);
  }
  for (const name of ['first_image_url', 'last_image_url']) if (req[name] !== undefined) out[name] = mediaUrl(req[name]);
  if (out.last_image_url && !out.first_image_url) throw new Error('last_image_url requires first_image_url');
  if (out.first_image_url && out.aspect_ratio !== 'adaptive') throw new Error('First-frame mode requires adaptive aspect_ratio');
  if (out.first_image_url && out.image_urls && out.image_urls.length) throw new Error('Choose first-frame or reference-image mode');
  if (req.kwargs !== undefined) {
    const kwargs = object(req.kwargs, 'kwargs');
    if (model === IMAGE || Object.keys(kwargs).some(key => key !== 'generate_audio')) throw new Error('Unsupported kwargs');
    if (kwargs.generate_audio !== undefined && typeof kwargs.generate_audio !== 'boolean') throw new Error('generate_audio must be boolean');
    out.kwargs = kwargs;
  }
  return out;
}
function upstream(ctx) {
  if (String(ctx.baseUrl).replace(/\/$/, '') !== 'https://task.likeai.pro/task-api') throw new Error('LikeAI requires the official HTTPS upstream');
  return 'https://task.likeai.pro/task-api';
}
function safeResult(body) {
  const result = body && body.data && body.data.result;
  if (!result || typeof result !== 'object') return {};
  const clean = {};
  for (const name of ['images', 'videos', 'covers', 'audios']) {
    if (Array.isArray(result[name]) && result[name].length <= MAX_IMAGE_COUNT) clean[name] = result[name].filter(url => typeof url === 'string' && /^https:\/\//.test(url));
  }
  return clean;
}
export const native = {
  create(ctx) {
    if (!ctx.body || ctx.body.kind !== 'json') throw new Error('JSON request required');
    const requestBody = normalize(ctx.body.value);
    return { kind: 'submit', model: requestBody.api_name, action: requestBody.api_name === IMAGE ? 'image' : 'video', requestBody };
  },
  created(_ctx, task) { return { code: 200, data: { task_id: task.task_id }, error: '' }; },
  query(_ctx, task) {
    const states = { NOT_START: 'queued', SUBMITTED: 'queued', QUEUED: 'queued', IN_PROGRESS: 'running', SUCCESS: 'completed', FAILURE: 'failed' };
    return { code: 200, data: { task_id: task.task_id, status: states[task.status] || 'unknown', message: task.status === 'FAILURE' ? '生成失败，请联系管理员核对任务记录' : '', result: safeResult(task.data) }, error: '' };
  },
};
export function buildSubmitRequest(ctx) {
  const body = normalize(ctx.requestBody);
  if ((ctx.upstreamModel || ctx.model) !== body.api_name) throw new Error('Model mapping is not supported by this adapter');
  return { url: upstream(ctx) + '/task/create_task', method: 'POST', headers: { 'Content-Type': 'application/json', 'X-API-Key': ctx.apiKey }, body };
}
export function parseSubmitResponse(ctx, response) {
  const body = response.body || {};
  if (body.code !== 200 || !body.data || typeof body.data.task_id !== 'string' || !body.data.task_id) throw new Error('LikeAI did not return an accepted task ID; do not automatically retry');
  return { taskId: body.data.task_id, taskData: { data: { status: 'queued' } }, state: { requested: { api_name: ctx.requestBody.api_name, resolution: ctx.requestBody.resolution, duration: ctx.requestBody.duration } } };
}
export function buildQueryRequest(ctx) {
  return { url: upstream(ctx) + '/task/query_task/' + encodeURIComponent(ctx.taskId), method: 'GET', headers: { 'X-API-Key': ctx.apiKey } };
}
export function parseTaskResult(_ctx, body) {
  if (!body || body.code !== 200 || !body.data) return { status: 'UNKNOWN', reason: 'Unrecognized LikeAI response' };
  const status = STATUS[body.data.status] || 'UNKNOWN';
  const result = { status };
  if (status === 'FAILURE') result.reason = 'LikeAI task failed';
  if (status === 'SUCCESS') {
    const media = safeResult(body);
    const url = (media.videos || [])[0] || (media.images || [])[0];
    if (!url) return { status: 'UNKNOWN', reason: 'Completed task has no media result' };
    result.url = url;
  }
  return result;
}
export function extractUsage(ctx) {
  if (ctx.usagePurpose === 'billing_ratios') return {};
  const req = normalize(ctx.requestBody);
  if (req.api_name === IMAGE) return { image_count: 1, resolution: req.resolution };
  return { seconds: req.duration, resolution: req.resolution };
}
export function extractUsageOnComplete(ctx, _taskResult, body) {
  const req = ctx.state && ctx.state.requested;
  if (!req) return {};
  if (req.api_name === IMAGE) {
    const count = (safeResult(body).images || []).length;
    if (count > 0 && count <= MAX_IMAGE_COUNT) return { image_count: count, resolution: req.resolution };
    return {};
  }
  return { seconds: req.duration, resolution: req.resolution };
}
