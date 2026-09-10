<template>
  <div class="generate-page">
    <el-card class="main-card">
      <template #header>
        <span class="card-title">生成service CRUD代码</span>
      </template>

      <el-form label-width="120px" label-position="right">
        <!-- 数据库 -->
        <el-form-item label="数据库" required>
          <el-select v-model="dbDriver" placeholder="选择数据库驱动" style="width:100%">
            <el-option v-for="d in dbDrivers" :key="d.value" :label="d.label" :value="d.value" />
          </el-select>
        </el-form-item>

        <!-- 数据库dsn -->
        <el-form-item label="数据库dsn" required>
          <el-input v-model="dsn" placeholder="root:123456@(127.0.0.1:3306)/dbname">
            <template #append>
              <el-button :loading="loading.tables" @click="loadTables">获取表名</el-button>
            </template>
          </el-input>
        </el-form-item>

        <!-- 表名 -->
        <el-form-item v-if="tables.length > 0" label="表名" required>
          <el-select
            v-model="selectedTables"
            multiple
            filterable
            placeholder="请选择表名，支持多选"
            style="width:100%"
            collapse-tags
            collapse-tags-tooltip
          >
            <div style="padding:8px 12px;border-bottom:1px solid #e4e7ed;">
              <el-checkbox
                :model-value="isAllSelected"
                :indeterminate="isIndeterminate"
                @change="toggleAll"
              >全选</el-checkbox>
            </div>
            <el-option v-for="t in tables" :key="t.value" :label="t.label" :value="t.value" />
          </el-select>
        </el-form-item>

        <!-- 服务名称 -->
        <el-form-item label="服务名称" required>
          <el-input v-model="form.serverName" placeholder="服务名称" />
        </el-form-item>

        <!-- module名称 -->
        <el-form-item label="module名称" required>
          <el-input v-model="form.moduleName" placeholder="go.mod 中的 module 名称" />
        </el-form-item>

        <!-- 嵌入Model -->
        <el-form-item label=" ">
          <div style="display:flex;align-items:center;gap:8px;">
            <el-switch v-model="form.embed" />
            <span>嵌入Model</span>
            <el-tooltip placement="right">
              <template #content>
                gorm.Model结构体字段对应表的id、created_at、updated_at、deleted_at 这4个列名，支持软删除。<br/>如果表包含这些列名，请开启嵌入Model，<br/>如果表不包含这些列名，请关闭嵌入Model。
              </template>
              <el-icon style="color:#c0c4cc;cursor:pointer;"><QuestionFilled /></el-icon>
            </el-tooltip>
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
import { QuestionFilled } from '@element-plus/icons-vue'
import { useCodeGenerator } from '../../composables/useCodeGenerator.js'

const {
  form, dbDrivers, dbDriver, dsn, tables, selectedTables,
  isAllSelected, isIndeterminate, toggleAll, loadTables,
  previewResult, loading, canGenerate,
  previewCode, generateCode, copyCommand,
} = useCodeGenerator({
  command: 'micro service',
  buildArgs: (f, { dbDriver, dsn, tables }) => {
    const args = []
    if (f.moduleName) args.push(`--module-name=${f.moduleName}`)
    if (f.serverName) args.push(`--server-name=${f.serverName}`)
    if (dbDriver) args.push(`--db-driver=${dbDriver}`)
    if (dsn) args.push(`--db-dsn=${dsn}`)
    if (tables.length) args.push(`--db-table=${tables.join(',')}`)
    args.push(`--embed=${f.embed}`)
    args.push('--suited-mono-repo=false')
    args.push('--extended-api=true')
    return args
  },
  canSubmit: (f, { dbDriver, dsn, tables }) => dbDriver && dsn && tables.length > 0 && f.serverName?.trim() !== '' && f.moduleName?.trim() !== '',
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
