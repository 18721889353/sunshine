/**
 * useCodeGenerator — 代码生成核心业务 Hook
 *
 * 封装：命令构建 → 预览(getTemplateInfo + --only-print) → 生成(fetch zip blob)
 * 每个页面传入自己的 { command, fields } 即可获得完整生成能力。
 */
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { listDrivers, listTables, getTemplateInfo, uploadFiles as apiUploadFiles, getConfig } from '../api'

/**
 * @param {Object} options
 * @param {string} options.command - CLI 子命令，如 'web http'、'micro rpc'、'cache'
 * @param {string} options.title - 页面标题
 * @param {Array}  options.fields - FormConfig 字段定义数组
 * @param {Function} options.buildArgs - (form, extra) => string[] 自定义参数构建
 * @param {Function} options.canSubmit - (form, extra) => boolean 自定义提交校验
 * @param {Function} options.resetForm - () => Object 重置表单（特殊页面用）
 */
export function useCodeGenerator(options) {
  const { command, title, fields = [], buildArgs, canSubmit, resetForm } = options

  // ======================== 表单状态 ========================
  const defaultForm = () => ({
    // 通用字段
    moduleName: '',
    serverName: '',
    projectName: '',
    repoAddr: '',
    outPath: '',
    embed: true,
    extendedApi: true,
    suitedMonoRepo: false,
    jsonNameType: 1,
    includeInitDb: false,
    rpcServerNames: '',
    webType: true,
  })

  const form = ref(defaultForm())

  // 数据库相关
  const dbDrivers = ref([])
  const dbDriver = ref('mysql')
  const dsn = ref('')
  const tables = ref([])
  const selectedTables = ref([])

  // ======================== 数据库持久化（localStorage）========================
  const DB_STORAGE_KEY = 'sunshine_db_config'
  const loadDbConfig = () => {
    try {
      const saved = localStorage.getItem(DB_STORAGE_KEY)
      if (saved) {
        const cfg = JSON.parse(saved)
        if (cfg.driver) dbDriver.value = cfg.driver
        if (cfg.dsn) dsn.value = cfg.dsn
      }
    } catch { /* ignore */ }
  }
  const saveDbConfig = () => {
    try {
      localStorage.setItem(DB_STORAGE_KEY, JSON.stringify({
        driver: dbDriver.value,
        dsn: dsn.value,
      }))
    } catch { /* ignore */ }
  }

  // Proto 文件（支持多个）
  const protoFiles = ref([])

  // 预览/生成状态
  const previewResult = ref('')
  const loading = reactive({ tables: false, preview: false, generate: false })

  // ======================== 数据库操作 ========================
  const loadDrivers = async () => {
    try {
      const data = await listDrivers()
      dbDrivers.value = data || []
    } catch (e) {
      ElMessage.error('加载数据库驱动失败: ' + e.message)
    }
  }

  const loadTables = async () => {
    if (!dsn.value) {
      ElMessage.warning('请输入数据库连接地址')
      return
    }
    loading.tables = true
    try {
      const data = await listTables(dsn.value, dbDriver.value)
      tables.value = data || []
      selectedTables.value = []
    } catch (e) {
      ElMessage.error('加载表列表失败: ' + e.message)
    } finally {
      loading.tables = false
    }
  }

  // ======================== 表全选 ========================
  const isAllSelected = computed(() =>
    tables.value.length > 0 && selectedTables.value.length === tables.value.length
  )
  const isIndeterminate = computed(() =>
    selectedTables.value.length > 0 && selectedTables.value.length < tables.value.length
  )
  const toggleAll = (val) => {
    selectedTables.value = val ? tables.value.map(t => t.value) : []
  }

  // ======================== 命令构建 ========================
  const buildCommand = () => {
    if (buildArgs) {
      const args = buildArgs(form.value, {
        dbDriver: dbDriver.value,
        dsn: dsn.value,
        tables: selectedTables.value,
        protoFiles: protoFiles.value,
      })
      return `sunshine ${command} ${args.join(' ')}`.trim()
    }
    // 默认：只传通用参数
    return `sunshine ${command}`.trim()
  }

  const buildArg = () => buildCommand().replace(/^sunshine\s+/, '')

  // ======================== 上传文件 ========================
  const uploadFileIfNeeded = async () => {
    if (protoFiles.value.length > 0) {
      return await apiUploadFiles(protoFiles.value)
    }
    return ''
  }

  // ======================== 预览（仅显示命令，不实际执行）========================
  const previewCode = async () => {
    loading.preview = true
    try {
      // 仅构建命令字符串，不调用后端接口
      const command = buildCommand()
      previewResult.value = command
    } catch (e) {
      ElMessage.error('预览失败: ' + e.message)
    } finally {
      loading.preview = false
    }
  }

  // ======================== 生成代码（fetch blob zip）========================
  const generateCode = async () => {
    try {
      await ElMessageBox.confirm('确定要生成代码吗？生成的代码将打包为 zip 文件下载。', '确认生成', { type: 'info' })
    } catch { return }

    loading.generate = true
    try {
      const arg = buildArg()
      // path 使用 command 部分（用 - 连接），用于文件名生成
      const path = (await uploadFileIfNeeded()) || command.replace(/\s+/g, '-') || '.'
      const baseURL = getConfig()

      const response = await fetch(baseURL + '/generate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ arg, path })
      })

      if (!response.ok) {
        throw new Error(response.headers.get('err-msg') || '生成失败')
      }

      const blob = await response.blob()
      // 构建文件名：moduleName-command-时间戳.zip
      const now = new Date()
      const timeStr = `${now.getHours().toString().padStart(2, '0')}${now.getMinutes().toString().padStart(2, '0')}${now.getSeconds().toString().padStart(2, '0')}`
      const cmdName = command.replace(/\s+/g, '-')
      const moduleName = form.value.moduleName || 'output'
      const filename = `${moduleName}-${cmdName}-${timeStr}.zip`

      const url = window.URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = filename
      document.body.appendChild(a)
      a.click()
      window.URL.revokeObjectURL(url)
      document.body.removeChild(a)
      ElMessage.success('代码生成成功，文件已下载')
    } catch (e) {
      ElMessage.error('生成代码失败: ' + e.message)
    } finally {
      loading.generate = false
    }
  }

  // ======================== 复制 ========================
  const copyCommand = async () => {
    try {
      await navigator.clipboard.writeText(previewResult.value)
      ElMessage.success('已复制到剪贴板')
    } catch {
      ElMessage.error('复制失败')
    }
  }

  // ======================== 提交校验 ========================
  const canGenerate = computed(() => {
    if (!canSubmit) return false
    // 直接访问 reactive 值，确保 computed 正确追踪依赖
    const f = form.value
    const t = selectedTables.value
    const p = protoFiles.value
    const d = dbDriver.value
    const dsnVal = dsn.value
    return canSubmit(f, { tables: t, protoFiles: p, dbDriver: d, dsn: dsnVal })
  })

  // ======================== 重置 ========================
  const reset = () => {
    form.value = resetForm ? resetForm() : defaultForm()
    dbDriver.value = 'mysql'
    dsn.value = ''
    tables.value = []
    selectedTables.value = []
    protoFiles.value = []
    previewResult.value = ''
  }

  // ======================== 初始化 ========================
  onMounted(() => {
    loadDrivers()
    loadDbConfig()
  })

  // 监听数据库配置变化，自动保存
  watch([dbDriver, dsn], saveDbConfig)

  return {
    // 表单
    form,
    fields,
    // 数据库
    dbDrivers, dbDriver, dsn,
    tables, selectedTables,
    isAllSelected, isIndeterminate, toggleAll, loadTables,
    // Proto
    protoFiles,
    // 状态
    previewResult, loading, canGenerate,
    // 操作
    previewCode, generateCode, copyCommand, buildCommand, reset,
  }
}
