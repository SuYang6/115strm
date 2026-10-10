<template>
  <router-view v-if="$route.path === '/login'" />
  <div v-else class="shell">
    <aside class="sidebar">
      <div class="brand">
        <div class="brand-mark">115</div>
        <div>
          <strong>115strm</strong>
          <span>115 STRM Console</span>
        </div>
      </div>
      <nav>
        <RouterLink v-for="item in nav" :key="item.path" :to="item.path" class="nav-item">
          <component :is="item.icon" :size="18" />
          <span>{{ item.label }}</span>
        </RouterLink>
      </nav>
    </aside>
    <main class="workspace">
      <header class="topbar">
        <div>
          <p class="eyebrow">当前页面</p>
          <h1>{{ currentTitle }}</h1>
        </div>
        <div class="top-actions">
          <el-tag effect="plain" type="success">8585</el-tag>
          <el-button :icon="LogOut" @click="auth.logout()">退出</el-button>
        </div>
      </header>
      <RouterView v-slot="{ Component }">
        <KeepAlive include="Dashboard,Browse,Strm302,Tasks,Recycle,Settings,Logs">
          <component :is="Component" />
        </KeepAlive>
      </RouterView>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, KeepAlive } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import { Archive, FolderTree, LayoutDashboard, ListTodo, LogOut, ScrollText, Settings, WandSparkles } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const route = useRoute()
const nav = [
  { path: '/dashboard', label: '总览', icon: LayoutDashboard },
  { path: '/browse', label: '115 浏览', icon: FolderTree },
  { path: '/strm302', label: 'STRM 生成', icon: WandSparkles },
  { path: '/tasks', label: '定时任务', icon: ListTodo },
  { path: '/recycle', label: '回收站', icon: Archive },
  { path: '/settings', label: '设置', icon: Settings },
  { path: '/logs', label: '日志', icon: ScrollText }
]

const currentTitle = computed(() => nav.find((item) => item.path === route.path)?.label || '115strm')
</script>
