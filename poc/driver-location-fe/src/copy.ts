export type Language = "zh" | "en" | "km";

const en = {
  appTitle: "Dr.life+ Driver Location",
  appSubtitle: "P0-9 PoC: capture, offline queue, and recovery",
  online: "Online",
  offline: "Offline",
  pendingLabel: "Pending",
  uploadedLabel: "Uploaded this session",
  lastCapturedLabel: "Last fix",
  lastErrorLabel: "Last error",
  intervalLabel: "Capture interval",
  queueTitle: "Local upload queue",
  queueEmpty: "No pending location points",
  retryNow: "Retry upload",
  retryBusy: "Uploading…",
  modeGps: "Real GPS",
  modeSimulated: "Simulated fix",
  versionLabel: "Page build",
  noError: "None",
  noLocation: "No fix yet",
  samplingEvery: "Captures every {seconds}s",
  testNotice:
    "PoC talks only to the local mock backend. Offline reload uses the Service Worker cache.",
  uploadFailed: "Upload failed; the point remains in the local queue",
  offlineHint: "Offline: new points are kept locally",
  permissionDenied: "Location permission denied or fix failed",
  geolocationUnavailable: "Geolocation is unavailable",
  flushDone: "Uploaded {count} queued points",
  languageAria: "Language",
  zhName: "中文",
  enName: "English",
  kmName: "ខ្មែរ"
} as const;

export type CopyKey = keyof typeof en;

const zh: Record<CopyKey, string> = {
  appTitle: "Dr.life+ 司机端定位",
  appSubtitle: "P0-9 PoC：定位采集、离线暂存与恢复补传",
  online: "在线",
  offline: "离线",
  pendingLabel: "待补传",
  uploadedLabel: "本次已提交",
  lastCapturedLabel: "最近定位",
  lastErrorLabel: "最近错误",
  intervalLabel: "采集间隔",
  queueTitle: "本地待补传队列",
  queueEmpty: "暂无待补传定位点",
  retryNow: "立即补传",
  retryBusy: "正在补传…",
  modeGps: "真实 GPS",
  modeSimulated: "模拟定位",
  versionLabel: "页面版本",
  noError: "无",
  noLocation: "尚无定位",
  samplingEvery: "每 {seconds} 秒采集一次",
  testNotice:
    "PoC 仅连接本机 mock 后端，不连接任何真实服务；离线刷新依赖 Service Worker 缓存。",
  uploadFailed: "上报失败，定位点已保留在本地队列",
  offlineHint: "当前离线，定位点会先保存在本地",
  permissionDenied: "定位权限被拒绝或定位失败",
  geolocationUnavailable: "浏览器不支持定位",
  flushDone: "已补传 {count} 个定位点",
  languageAria: "语言",
  zhName: "中文",
  enName: "English",
  kmName: "ខ្មែរ"
};

const km: Record<CopyKey, string> = {
  appTitle: "Dr.life+ អ្នកបើកបរ",
  appSubtitle:
    "P0-9 PoC៖ ការចាប់យកទីតាំង ការរក្សាទុកក្រៅបណ្តាញ និងការផ្ញើឡើងវិញ",
  online: "អនឡាញ",
  offline: "អត់អនឡាញ",
  pendingLabel: "រង់ចាំផ្ញើ",
  uploadedLabel: "បានផ្ញើវគ្គនេះ",
  lastCapturedLabel: "ទីតាំងចុងក្រោយ",
  lastErrorLabel: "កំហុសចុងក្រោយ",
  intervalLabel: "ចន្លោះពេលចាប់យក",
  queueTitle: "ជួរផ្ញើក្នុងឧបករណ៍",
  queueEmpty: "មិនមានទីតាំងរង់ចាំផ្ញើ",
  retryNow: "ព្យាយាមផ្ញើឡើងវិញ",
  retryBusy: "កំពុងផ្ញើ…",
  modeGps: "GPS ពិត",
  modeSimulated: "ទីតាំងសាកល្បង",
  versionLabel: "កំណែទំព័រ",
  noError: "គ្មាន",
  noLocation: "មិនទាន់មានទីតាំង",
  samplingEvery: "ចាប់យករាល់ {seconds} វិនាទី",
  testNotice:
    "PoC ភ្ជាប់តែម៉ាស៊ីនមេសាកល្បងក្នុងឧបករណ៍ប៉ុណ្ណោះ។ ការផ្ទុកឡើងវិញពេលអត់អនឡាញប្រើ Service Worker។",
  uploadFailed: "ការផ្ញើបរាជ័យ ទីតាំងនៅរង់ចាំក្នុងជួរក្នុងឧបករណ៍",
  offlineHint: "អត់អនឡាញ៖ ទីតាំងថ្មីត្រូវរក្សាទុកក្នុងឧបករណ៍",
  permissionDenied: "សិទ្ធិទីតាំងត្រូវបានបដិសេធ ឬចាប់យកបរាជ័យ",
  geolocationUnavailable: "កម្មវិធីរុករកមិនគាំទ្រទីតាំង",
  flushDone: "បានផ្ញើ {count} ចំណុច",
  languageAria: "ភាសា",
  zhName: "中文",
  enName: "English",
  kmName: "ខ្មែរ"
};

const dictionaries: Record<Language, Record<CopyKey, string>> = {
  zh,
  en,
  km
};

export function text(
  language: Language,
  key: CopyKey,
  params: Record<string, string | number> = {}
): string {
  return Object.entries(params).reduce(
    (value, [name, replacement]) =>
      value.replaceAll(`{${name}}`, String(replacement)),
    dictionaries[language][key]
  );
}

export function isLanguage(value: string | null): value is Language {
  return value === "zh" || value === "en" || value === "km";
}
