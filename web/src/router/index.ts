import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import Login from '@/views/Login.vue'
import Dashboard from '@/views/Dashboard.vue'
import Browse from '@/views/Browse.vue'
import Strm302 from '@/views/Strm302.vue'
import Tasks from '@/views/Tasks.vue'
import Settings from '@/views/Settings.vue'
import Logs from '@/views/Logs.vue'
import Recycle from '@/views/Recycle.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/dashboard' },
    { path: '/login', component: Login, meta: { public: true } },
    { path: '/dashboard', component: Dashboard },
    { path: '/browse', component: Browse },
    { path: '/strm302', component: Strm302 },
    { path: '/tasks', component: Tasks },
    { path: '/recycle', component: Recycle },
    { path: '/settings', component: Settings },
    { path: '/logs', component: Logs }
  ]
})

router.beforeEach((to) => {
  const auth = useAuthStore()
  if (!to.meta.public && !auth.token) return '/login'
})

export default router
