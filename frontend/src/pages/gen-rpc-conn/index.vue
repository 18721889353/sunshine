<template>
  <div class="generate-page">
    <el-card class="main-card">
      <template #header>
        <span class="card-title">生成grpc服务连接代码</span>
      </template>

      <el-form label-width="120px" label-position="right">
        <!-- module名称 -->
        <el-form-item label="module名称" required>
          <el-input v-model="form.moduleName" placeholder="Go 模块名" />
        </el-form-item>

        <!-- grpc服务名称 -->
        <el-form-item label="grpc服务名称" required>
          <el-input v-model="form.rpcServerNames" placeholder="多个用逗号分隔" />
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 操作按钮 -->
    <div style="display: flex; gap: 10px; margin-top: 20px">
      <el-button type="primary" :disabled="!canGenerate" :loading="loading.preview" @click="previewCode"
        >预览命令</el-button
      >
      <el-button type="success" :disabled="!canGenerate" :loading="loading.generate" @click="generateCode"
        >生成代码</el-button
      >
    </div>

    <!-- 预览结果 -->
    <el-card v-if="previewResult" class="config-card" style="margin-top: 20px">
      <template #header>
        <div style="display: flex; justify-content: space-between; align-items: center">
          <span>预览结果</span>
          <el-button type="primary" size="small" link @click="copyCommand">复制</el-button>
        </div>
      </template>
      <pre style="margin: 0; white-space: pre-wrap; word-break: break-all; font-size: 13px">{{ previewResult }}</pre>
    </el-card>
  </div>
</template>

<script setup>
import { useCodeGenerator } from '../../composables/useCodeGenerator.js'

const { form, previewResult, loading, canGenerate, previewCode, generateCode, copyCommand } = useCodeGenerator({
  command: 'micro rpc-conn',
  buildArgs: (f) => {
    const args = []
    if (f.moduleName) args.push(`--module-name=${f.moduleName}`)
    if (f.rpcServerNames) args.push(`--rpc-server-name=${f.rpcServerNames}`)
    args.push('--suited-mono-repo=false')
    return args
  },
  canSubmit: (f) => f.rpcServerNames?.trim() !== '' && f.moduleName?.trim() !== '',
})
</script>

<style scoped>
.generate-page {
  max-width: 800px;
  margin: 0 auto;
  padding: 20px;
}
.main-card {
  margin-bottom: 20px;
}
.card-title {
  font-size: 16px;
  font-weight: 500;
}
.config-card {
  margin-bottom: 20px;
}
</style>
