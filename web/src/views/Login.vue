<template>
  <main class="login-screen">
    <section class="login-card">
      <div class="brand login-brand">
        <div class="brand-mark">115</div>
        <div>
          <strong>115strm</strong>
          <span>115 STRM Console</span>
        </div>
      </div>
      <h1>登录控制台</h1>
      <p>用于管理 115 登录、STRM 生成和本地任务。</p>
      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item label="用户名">
          <el-input v-model="username" size="large" autocomplete="username" @keyup.enter="submit" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="password" size="large" type="password" show-password autocomplete="current-password" @keyup.enter="submit" />
        </el-form-item>
        <el-button type="primary" size="large" class="login-button" :loading="loading" @click="submit">登录</el-button>
      </el-form>
    </section>
  </main>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const username = ref('admin')
const password = ref('')
const loading = ref(false)

async function submit() {
  loading.value = true
  try {
    await auth.login(username.value, password.value)
  } catch (error: any) {
    ElMessage.error(error.message)
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-screen { min-height: 100vh; display: grid; place-items: center; padding: 24px; background: linear-gradient(135deg, #f7f4ec, #e7f1ec); }
.login-card { width: min(440px, 100%); background: #fffdf8; border: 1px solid var(--line); border-radius: 8px; padding: 28px; box-shadow: 0 24px 60px rgba(47,64,55,.12); }
.login-brand { padding: 0 0 20px; }
h1 { margin: 0; font-size: 30px; }
p { margin: 8px 0 24px; color: var(--muted); }
.login-button { width: 100%; }
</style>
