<template>
  <section class="panel">
    <div class="toolbar">
      <el-input v-model="cid" placeholder="目录 CID" style="width: 260px" />
      <el-input v-model="sourcePath" placeholder="输出相对目录，例如 /电影" style="width: 320px" />
      <el-button type="primary" :icon="FolderOpen" @click="manualLoad">打开目录</el-button>
      <el-button :disabled="pathStack.length <= 1" :icon="CornerUpLeft" @click="goUp">返回上层</el-button>
      <el-button :disabled="!selected.length" :icon="WandSparkles" @click="generateSelected">生成所选</el-button>
      <el-button :disabled="!cid || cid === '0'" @click="generateDirectory">递归生成目录</el-button>
    </div>
    <div class="browse-path">
      <span class="muted">当前目录</span>
      <el-breadcrumb separator="/">
        <el-breadcrumb-item v-for="(item, index) in pathStack" :key="`${item.cid}-${index}`">
          <button class="crumb-link" type="button" @click="goCrumb(index)">{{ item.name }}</button>
        </el-breadcrumb-item>
      </el-breadcrumb>
    </div>
    <el-table :data="files" border height="calc(100vh - 230px)" empty-text="请输入 CID 后点击打开目录" @selection-change="selected = $event">
      <el-table-column type="selection" width="44" />
      <el-table-column prop="name" label="名称" min-width="320">
        <template #default="{ row }">
          <el-button v-if="row.is_dir" link type="primary" @click="enter(row)">{{ row.name }}</el-button>
          <span v-else>{{ row.name }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="pickcode" label="pickcode" width="190">
        <template #default="{ row }">
          <span class="code">{{ row.pickcode || '-' }}</span>
          <el-button v-if="row.pickcode" link @click="copy(row.pickcode)">复制</el-button>
        </template>
      </el-table-column>
      <el-table-column prop="id" label="CID / 文件ID" width="230">
        <template #default="{ row }">
          <span class="code">{{ row.id || '-' }}</span>
          <el-button v-if="row.id" link @click="copyID(row)">{{ row.is_dir ? '复制 CID' : '复制文件ID' }}</el-button>
        </template>
      </el-table-column>
      <el-table-column prop="size" label="大小" width="120" />
    </el-table>
  </section>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { CornerUpLeft, FolderOpen, WandSparkles } from 'lucide-vue-next'
import { getData, postData } from '@/api/client'

defineOptions({ name: 'Browse' })

type PathItem = {
  name: string
  cid: string
}

const cid = ref('0')
const sourcePath = ref('')
const files = ref<any[]>([])
const selected = ref<any[]>([])
const pathStack = ref<PathItem[]>([{ name: '根目录', cid: '0' }])

async function load() {
  files.value = await getData('/115/browse', { cid: cid.value })
  selected.value = []
}
async function manualLoad() {
  const nextCID = (cid.value || '0').trim()
  cid.value = nextCID || '0'
  if (cid.value === '0') {
    pathStack.value = [{ name: '根目录', cid: '0' }]
    sourcePath.value = ''
  } else if (pathStack.value[pathStack.value.length - 1]?.cid !== cid.value) {
    pathStack.value = [{ name: '根目录', cid: '0' }, { name: `CID ${cid.value}`, cid: cid.value }]
  }
  await load()
}
async function enter(row: any) {
  cid.value = row.id
  pathStack.value.push({ name: row.name, cid: row.id })
  syncSourcePath()
  await load()
}
async function goCrumb(index: number) {
  const target = pathStack.value[index]
  if (!target) return
  pathStack.value = pathStack.value.slice(0, index + 1)
  cid.value = target.cid
  syncSourcePath()
  await load()
}
async function goUp() {
  if (pathStack.value.length <= 1) return
  await goCrumb(pathStack.value.length - 2)
}
function syncSourcePath() {
  const names = pathStack.value.slice(1).map((x) => x.name)
  sourcePath.value = names.length ? `/${names.join('/')}` : ''
}
async function copy(text: string) {
  await writeClipboard(text)
  ElMessage.success('已复制 pickcode')
}
async function copyID(row: any) {
  await writeClipboard(String(row.id || ''))
  ElMessage.success(row.is_dir ? '已复制 CID' : '已复制文件ID')
}
async function writeClipboard(text: string) {
  if (!text) return
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return
    } catch {
      // HTTP/IP access can block the Clipboard API. Fall back to a temporary textarea.
    }
  }
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.setAttribute('readonly', 'true')
  textarea.style.position = 'fixed'
  textarea.style.left = '-9999px'
  textarea.style.top = '0'
  document.body.appendChild(textarea)
  textarea.focus()
  textarea.select()
  const ok = document.execCommand('copy')
  document.body.removeChild(textarea)
  if (!ok) throw new Error('复制失败，请手动复制')
}
async function generateSelected() {
  const pickcodes = selected.value.filter((x) => !x.is_dir && x.pickcode).map((x) => x.pickcode)
  if (!pickcodes.length) return ElMessage.warning('请选择文件')
  const res: any = await postData('/strm302/generate', { type: 'pickcode_list', pickcodes })
  ElMessage.success(`任务已创建：${res.job_id}`)
}
async function generateDirectory() {
  const res: any = await postData('/strm302/generate', { type: 'own_directory_recursive', cid: cid.value, source_path: sourcePath.value })
  ElMessage.success(`递归生成任务已创建：${res.job_id}`)
}
</script>

<style scoped>
.browse-path {
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 38px;
  margin: -2px 0 12px;
  padding: 0 2px;
}
.crumb-link {
  border: 0;
  padding: 0;
  background: transparent;
  color: var(--primary);
  cursor: pointer;
  font: inherit;
}
.crumb-link:hover { text-decoration: underline; }
</style>
