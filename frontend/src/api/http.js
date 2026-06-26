import axios from 'axios'

export const http = axios.create({
  baseURL: '/api',
  timeout: 30000
})

http.interceptors.response.use(
  (response) => {
    const body = response.data
    if (body && typeof body === 'object' && 'code' in body) {
      if (body.code !== 0) {
        throw new Error(body.message || '请求失败')
      }
      return body.data
    }
    return body
  },
  (error) => {
    const message = error?.response?.data?.message || error?.message || '网络请求失败'
    throw new Error(message)
  }
)
