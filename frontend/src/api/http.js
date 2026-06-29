import axios from "axios";
import { ElNotification } from "element-plus";

export const http = axios.create({
  baseURL: "/api",
  timeout: 30000,
});

let lastAlertKey = "";
let lastAlertAt = 0;

http.interceptors.response.use(
  (response) => {
    const body = response.data;
    if (body && typeof body === "object" && "code" in body) {
      if (body.code !== 0) {
        const message = body.message || body.error || "请求失败";
        notifyBackendError(message, body.traceId);
        throw new Error(message);
      }
      return body.data;
    }
    return body;
  },
  (error) => {
    const body = error?.response?.data;
    const message =
      body?.message || body?.error || error?.message || "网络请求失败";
    notifyBackendError(message, body?.traceId);
    throw new Error(message);
  },
);

function notifyBackendError(message, traceId) {
  const normalized = String(message || "请求失败").trim();
  const key = `${normalized}|${traceId || ""}`;
  const now = Date.now();
  if (key === lastAlertKey && now - lastAlertAt < 2500) return;
  lastAlertKey = key;
  lastAlertAt = now;
  ElNotification({
    title: "后端接口异常",
    message: traceId ? `${normalized} · Trace ${traceId}` : normalized,
    type: "error",
    position: "top-right",
    duration: 7000,
    customClass: "api-error-notification",
  });
}
