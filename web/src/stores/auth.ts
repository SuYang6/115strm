import { defineStore } from 'pinia'
import { api } from '@/api/client'
import router from '@/router'

interface User {
  id: number
  username: string
}

/** 将旧品牌保存的登录凭证迁移到当前缓存键，避免改名后丢失会话。 */
function readStoredToken(): string {
  const token = localStorage.getItem('115strm_token') || localStorage.getItem('panstr_token') || ''
  if (token) localStorage.setItem('115strm_token', token)
  localStorage.removeItem('panstr_token')
  return token
}

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: readStoredToken(),
    user: null as User | null
  }),
  actions: {
    async login(username: string, password: string) {
      const res: any = await api.post('/auth/login', { username, password })
      this.token = res.data.token
      this.user = res.data.user
      localStorage.setItem('115strm_token', this.token)
      await router.push('/dashboard')
    },
    async loadMe() {
      if (!this.token) return
      const res: any = await api.get('/auth/me')
      this.user = res.data
    },
    logout() {
      this.token = ''
      this.user = null
      localStorage.removeItem('115strm_token')
      if (router.currentRoute.value.path !== '/login') router.push('/login')
    }
  }
})
