import axios from 'axios'
import { useAuthStore } from '@/stores/auth'

export const api = axios.create({
  baseURL: '/api',
  timeout: 30000
})

api.interceptors.request.use((config) => {
  const auth = useAuthStore()
  if (auth.token) {
    config.headers.Authorization = `Bearer ${auth.token}`
  }
  return config
})

api.interceptors.response.use(
  (response) => response.data,
  (error) => {
    const message = error.response?.data?.error || error.message || '请求失败'
    if (error.response?.status === 401) {
      const auth = useAuthStore()
      auth.logout()
    }
    return Promise.reject(new Error(message))
  }
)

export async function getData<T>(url: string, params?: any): Promise<T> {
  const res: any = await api.get(url, { params })
  return res.data as T
}

export async function postData<T>(url: string, data?: any): Promise<T> {
  const res: any = await api.post(url, data)
  return res.data as T
}

export async function putData<T>(url: string, data?: any): Promise<T> {
  const res: any = await api.put(url, data)
  return res.data as T
}

export async function deleteData<T>(url: string): Promise<T> {
  const res: any = await api.delete(url)
  return res.data as T
}
