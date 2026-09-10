<template>
  <div class="generate-page">
    <el-card class="main-card">
      <template #header>
        <span class="card-title">生成cache代码</span>
      </template>

      <el-form label-width="120px" label-position="right">
        <!-- module名称 -->
        <el-form-item label="module名称" required>
          <el-input v-model="form.moduleName" placeholder="Go 模块名" />
        </el-form-item>

        <!-- 缓存名称 -->
        <el-form-item label="缓存名称" required>
          <el-input v-model="form.cacheName" placeholder="例如: userToken" />
        </el-form-item>

        <!-- key名称 + key类型 -->
        <el-form-item label="key名称" required>
          <div style="display:flex;gap:12px;">
            <el-input v-model="form.keyName" placeholder="例如: id、uid" style="flex:1;" />
            <el-select v-model="form.keyType" placeholder="类型" style="width:120px;">
              <el-option v-for="t in typeOptions" :key="t" :label="t" :value="t" />
            </el-select>
          </div>
        </el-form-item>

        <!-- value名称 + value类型 -->
        <el-form-item label="value名称" required>
          <div style="display:flex;gap:12px;">
            <el-input v-model="form.valueName" placeholder="例如: token、data" style="flex:1;" />
            <el-select v-model="form.valueType" placeholder="类型" style="width:120px;">
              <el-option v-for="t in typeOptions" :key="t" :label="t" :value="t" />
            </el-select>
          </div>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 操作按钮 -->
    <div style="display:flex;gap:10px;margin-top:20px;">
      <el-button type="primary" :disabled="!canGenerate" :loading="loading.preview" @click="previewCode">预览命令</el-button>
      <el-button type="success" :disabled="!canGenerate" :loading="loading.generate" @click="generateCode">生成代码</el-button>
    </div>

    <!-- 预览结果 -->
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

const typeOptions = ['uint64', 'string', 'int', 'int64']

const {
  form,
  previewResult, loading, canGenerate,
  previewCode, generateCode, copyCommand,
} = useCodeGenerator({
  command: 'web cache',
  buildArgs: (f) => {
    const args = []
    if (f.moduleName) args.push(`--module-name=${f.moduleName}`)
    if (f.cacheName) args.push(`--cache-name=${f.cacheName}`)
    if (f.keyName) args.push(`--key-name=${f.keyName}`)
    if (f.keyType) args.push(`--key-type=${f.keyType}`)
    if (f.valueName) args.push(`--value-name=${f.valueName}`)
    if (f.valueType) args.push(`--value-type=${f.valueType}`)
    if (f.prefixKey) args.push(`--prefix-key=${f.prefixKey}`)
    args.push('--suited-mono-repo=false')
    return args
  },
  canSubmit: (f) => f.moduleName?.trim() && f.cacheName?.trim() && f.keyName?.trim() && f.keyType && f.valueName?.trim() && f.valueType,
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
