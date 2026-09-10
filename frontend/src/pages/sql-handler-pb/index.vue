<template>
  <div class="generate-page">
    <el-row :gutter="20">
      <el-col :span="12">
        <el-card class="config-card">
          <template #header><span>数据库连接</span></template>
          <el-form label-width="80px">
            <el-form-item label="驱动">
              <el-select v-model="dbDriver" placeholder="选择驱动" style="width:100%">
                <el-option v-for="d in dbDrivers" :key="d.value" :label="d.label" :value="d.value" />
              </el-select>
            </el-form-item>
            <el-form-item label="DSN">
              <el-input v-model="dsn" placeholder="root:123456@tcp(127.0.0.1:3306)/dbname" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="loading.tables" @click="loadTables">加载表结构</el-button>
            </el-form-item>
          </el-form>
        </el-card>
        <el-card v-if="tables.length > 0" class="config-card">
          <template #header>
            <div style="display:flex;justify-content:space-between;align-items:center;">
              <span>表名</span>
              <span style="color:#909399;font-size:12px;">已选 {{ selectedTables.length }} 个</span>
            </div>
          </template>
          <el-select v-model="selectedTables" multiple filterable placeholder="请选择表名，支持多选" style="width:100%" collapse-tags collapse-tags-tooltip>
            <div style="padding:8px 12px;border-bottom:1px solid #e4e7ed;">
              <el-checkbox :model-value="isAllSelected" :indeterminate="isIndeterminate" @change="toggleAll">全选</el-checkbox>
            </div>
            <el-option v-for="t in tables" :key="t.value" :label="t.label" :value="t.value" />
          </el-select>
        </el-card>
        <el-card class="config-card">
          <template #header><span>生成配置</span></template>
          <el-form label-width="100px">
            <FormField v-model="form.moduleName" label="模块名称" placeholder="go.mod 中的 module 名称" />
            <FormField v-model="form.serverName" label="服务名称" placeholder="服务名称" />
            <FormField v-model="form.outPath" label="输出路径" placeholder="输出目录（可选）" />
            <FormField v-model="form.jsonNameType" label="JSON 风格" type="radio" :options="[{ label: '驼峰', value: 1 }, { label: '下划线', value: 0 }]" />
            <el-form-item label="选项">
              <el-switch v-model="form.embed" />
              <span style="margin-left:8px">嵌入Model</span>
              <el-tooltip placement="right">
                <template #content>
                  gorm.Model结构体字段对应表的id、created_at、updated_at、deleted_at 这4个列名，支持软删除。<br/>如果表包含这些列名，请开启嵌入Model，<br/>如果表不包含这些列名，请关闭嵌入Model。
                </template>
                <el-icon style="color:#c0c4cc;cursor:pointer;margin-left:4px;"><QuestionFilled /></el-icon>
              </el-tooltip>
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
import { QuestionFilled } from '@element-plus/icons-vue'
import { useCodeGenerator } from '../../composables/useCodeGenerator.js'
import FormField from '../../components/FormField.vue'

const {
  form, dbDrivers, dbDriver, dsn, tables, selectedTables,
  isAllSelected, isIndeterminate, toggleAll, loadTables,
  previewResult, loading, canGenerate,
  previewCode, generateCode, copyCommand,
} = useCodeGenerator({
  command: 'web handler-pb',
  buildArgs: (f, { dbDriver, dsn, tables }) => {
    const args = []
    if (dbDriver) args.push(`--db-driver=${dbDriver}`)
    if (dsn) args.push(`--db-dsn=${dsn}`)
    if (tables.length) args.push(`--db-table=${tables.join(',')}`)
    if (f.moduleName) args.push(`--module-name=${f.moduleName}`)
    if (f.serverName) args.push(`--server-name=${f.serverName}`)
    if (f.outPath) args.push(`--out=${f.outPath}`)
    args.push(`--json-name-type=${f.jsonNameType ?? 1}`)
    if (f.embed) args.push('--embed')
    args.push('--extended-api')
    if (f.suitedMonoRepo) args.push('--suited-mono-repo')
    return args
  },
  canSubmit: (f, { tables }) => tables.length > 0,
})
</script>

<style scoped>
.config-card { margin-bottom: 20px; }
.action-card { margin-bottom: 20px; }
</style>
