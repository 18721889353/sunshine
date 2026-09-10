<template>
  <div class="generate-page">
    <el-row :gutter="20">
      <el-col :span="12">
        <el-card class="config-card">
          <template #header><span>Protobuf 配置</span></template>
          <el-form label-width="120px">
            <el-form-item label="文件">
              <el-upload class="upload-demo" drag :auto-upload="false" :on-change="handleFileChange" :on-remove="handleFileRemove" accept=".proto,.yml,.yaml" multiple>
                <el-icon class="el-icon--upload"><upload-filled /></el-icon>
                <div class="el-upload__text">拖拽文件到此处或 <em>点击上传</em></div>
                <template #tip>
                  <div class="el-upload__tip">支持 .proto / .yml / .yaml，可多选</div>
                </template>
              </el-upload>
            </el-form-item>
          </el-form>
        </el-card>
        <el-card class="config-card">
          <template #header><span>生成配置</span></template>
          <el-form label-width="100px">
            <FormField v-model="form.moduleName" label="模块名称" placeholder="go.mod 中的 module 名称" />
            <FormField v-model="form.serverName" label="服务名称" placeholder="服务名称" />
            <FormField v-model="form.projectName" label="项目名称" placeholder="用于部署名称" />
            <FormField v-model="form.repoAddr" label="镜像仓库" placeholder="Docker 镜像仓库地址（可选）" />
            <FormField v-model="form.outPath" label="输出路径" placeholder="输出目录（可选）" />
            <el-form-item label="选项">
              <el-switch v-model="form.suitedMonoRepo" active-text="适配单体仓库" />
            </el-form-item>
          </el-form>
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card class="action-card">
          <template #header><span>操作</span></template>
          <div style="display:flex;gap:10px;">
            <el-button type="primary" :disabled="!canGenerate" :loading="loading.preview" @click="previewCode">预览命令</el-button>
            <el-button type="success" :disabled="!canGenerate" :loading="loading.generate" @click="generateCode">生成代码</el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>
    <el-card v-if="previewResult" class="config-card" style="margin-top:20px;">
      <template #header>
        <div style="display:flex;justify-content:space-between;align-items:center;">
          <span>预览结果</span>
          <el-button type="primary" size="small" link @click="copyCommand">复制</el-button>
        </div>
      </template>
      <pre style="margin:0;white-space:pre-wrap;word-break:break-all;font-size:13px;">{{ previewResult }}</pre>
    </el-card>
  </div>
</template>

<script setup>
import { UploadFilled } from '@element-plus/icons-vue'
import { useCodeGenerator } from '../../composables/useCodeGenerator.js'
import FormField from '../../components/FormField.vue'

const {
  form, protoFiles,
  previewResult, loading, canGenerate,
  previewCode, generateCode, copyCommand,
} = useCodeGenerator({
  command: 'micro rpc-gw-pb',
  buildArgs: (f, { protoFiles }) => {
    const args = []
    if (protoFiles.length) args.push('--protobuf-file=')
    if (f.moduleName) args.push(`--module-name=${f.moduleName}`)
    if (f.serverName) args.push(`--server-name=${f.serverName}`)
    if (f.projectName) args.push(`--project-name=${f.projectName}`)
    if (f.repoAddr) args.push(`--repo-addr=${f.repoAddr}`)
    if (f.outPath) args.push(`--out=${f.outPath}`)
    if (f.suitedMonoRepo) args.push('--suited-mono-repo')
    return args
  },
  canSubmit: (f, { protoFiles }) => protoFiles.length > 0,
})

const fileList = []
const handleFileChange = (file, newFileList) => {
  fileList.length = 0
  newFileList.forEach(f => fileList.push(f.raw))
  protoFiles.value = [...fileList]
}
const handleFileRemove = (file, newFileList) => {
  fileList.length = 0
  newFileList.forEach(f => fileList.push(f.raw))
  protoFiles.value = [...fileList]
}
</script>

<style scoped>
.config-card { margin-bottom: 20px; }
.action-card { margin-bottom: 20px; }
</style>
