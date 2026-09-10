<template>
  <div class="generate-page">
    <el-row :gutter="20">
      <el-col :span="12">
        <el-card class="config-card">
          <template #header><span>Protobuf 配置</span></template>
          <el-form label-width="100px">
            <FormField v-model="form.moduleName" label="模块名称" placeholder="go.mod 中的 module 名称" />
            <FormField v-model="form.serverName" label="服务名称" placeholder="服务名称（mono-repo 模式）" />
            <FormField v-model="form.outPath" label="输出路径" placeholder="输出目录（可选）" />
            <FormField v-model="form.jsonNameType" label="JSON 风格" type="radio" :options="[{ label: '驼峰', value: 1 }, { label: '下划线', value: 0 }]" />
            <el-form-item label="选项">
              <el-switch v-model="form.embed" active-text="嵌入 gorm.Model" />
              <el-switch v-model="form.extendedApi" active-text="扩展 CRUD API" style="margin-left:16px" />
              <el-switch v-model="form.suitedMonoRepo" active-text="适配单体仓库" style="margin-left:16px" />
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
import { useCodeGenerator } from '../../composables/useCodeGenerator.js'
import FormField from '../../components/FormField.vue'

const {
  form,
  previewResult, loading, canGenerate,
  previewCode, generateCode, copyCommand,
} = useCodeGenerator({
  command: 'micro proto',
  buildArgs: (f) => {
    const args = []
    if (f.moduleName) args.push(`--module-name=${f.moduleName}`)
    if (f.serverName) args.push(`--server-name=${f.serverName}`)
    if (f.outPath) args.push(`--out=${f.outPath}`)
    args.push(`--json-name-type=${f.jsonNameType ?? 1}`)
    if (f.embed) args.push('--embed')
    if (f.extendedApi) args.push('--extended-api')
    if (f.suitedMonoRepo) args.push('--suited-mono-repo')
    return args
  },
  canSubmit: () => true,
})
</script>

<style scoped>
.config-card { margin-bottom: 20px; }
.action-card { margin-bottom: 20px; }
</style>
