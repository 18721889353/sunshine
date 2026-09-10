import axios from 'axios'

// 从配置文件读取API地址
// 获取API基础地址
export const getConfig = () => {
  try {
    if (window.appConfig && window.appConfig.sunshineServiceAddr) {
      return window.appConfig.sunshineServiceAddr
    }
  } catch {
    console.warn('读取配置失败，使用默认地址')
  }
  return '/api/v1'
}

const api = axios.create({
  baseURL: getConfig(),
  timeout: 120000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// 响应拦截器
api.interceptors.response.use(
  (response) => {
    const data = response.data
    if (data.code === 0 || data.code === '0') {
      return data.data
    } else {
      return Promise.reject(new Error(data.msg || '请求失败'))
    }
  },
  (error) => {
    console.error('API请求错误:', error)
    return Promise.reject(error)
  }
)

/**
 * 获取数据库驱动列表
 * GET /api/v1/listDrivers
 * 返回: [{label: "mysql", value: "mysql"}]
 */
export const listDrivers = () => {
  return api.get('/listDrivers')
}

/**
 * 获取数据库表列表
 * POST /api/v1/listTables
 * 请求体: { dsn: string, dbDriver: string }
 * 返回: [{label: "table_name", value: "table_name"}]
 */
export const listTables = (dsn, dbDriver = 'mysql') => {
  return api.post('/listTables', { dsn, dbDriver })
}

/**
 * 生成代码（返回 zip 文件下载）
 * POST /api/v1/generate
 * 请求体: { arg: string, path: string }
 * 返回: 二进制 zip 文件
 */
export const generateCode = (arg, path = '') => {
  return api.post(
    '/generate',
    { arg, path },
    {
      responseType: 'blob',
      headers: { 'Content-Type': 'application/json' },
    }
  )
}

/**
 * 获取模板信息 / 预览命令输出
 * POST /api/v1/getTemplateInfo
 * 请求体: { arg: string, path: string }
 * 返回: JSON {code, msg, data}
 * 如果 arg 中包含 --only-print，后端会执行命令并返回输出结果
 */
export const getTemplateInfo = (arg, path = '') => {
  return api.post('/getTemplateInfo', { arg, path })
}

/**
 * 上传文件（.proto / .yml / .yaml）
 * POST /api/v1/uploadFiles
 * 请求体: multipart/form-data, field name = "file"
 * 返回: 上传后的文件路径（用于传给 --protobuf-file 或 --yaml-file）
 */
export const uploadFiles = (files) => {
  const formData = new FormData()
  files.forEach((file) => {
    formData.append('file', file)
  })
  return api.post('/uploadFiles', formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
}

/**
 * 获取历史生成记录
 * GET /api/v1/record/:path
 * 返回: 上次使用的 parameters
 */
export const getRecord = (path) => {
  return api.get(`/record/${path}`)
}

export default api
