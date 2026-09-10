<template>
  <div class="generate-page">
    <el-row :gutter="20">
      <!-- 左列：数据库 + 表选择 + 生成配置 -->
      <el-col :span="12">
        <!-- 数据库连接 -->
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

        <!-- 表选择 -->
        <el-card v-if="tables.length > 0" class="config-card">
          <template #header>
            <div style="display:flex;justify-content:space-between;align-items:center;">
              <span><span style="color:#f56c6c;">*</span> 表名</span>
              <span style="color:#909399;font-size:12px;">已选 {{ selectedTables.length }} 个</span>
            </div>
          </template>
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
        </el-card>

        <!-- Model 配置 -->
        <el-card class="config-card">
          <template #header><span>Model 配置</span></template>
          <el-form label-width="100px">
            <el-form-item label="选项">
              <el-switch v-model="form.embed" active-text="嵌入 Model" />
            </el-form-item>
          </el-form>
        </el-card>
      </el-col>

      <!-- 右列：操作按钮 -->
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
import FormField from '../../components/FormField.vue'

const {
  form, dbDrivers, dbDriver, dsn, tables, selectedTables,
  isAllSelected, isIndeterminate, toggleAll, loadTables,
  previewResult, loading, canGenerate,
  previewCode, generateCode, copyCommand,
} = useCodeGenerator({
  command: 'web model',
  buildArgs: (f, { dbDriver, dsn, tables }) => {
    const args = []
    if (dbDriver) args.push(`--db-driver=${dbDriver}`)
    if (dsn) args.push(`--db-dsn=${dsn}`)
    if (tables.length) args.push(`--db-table=${tables.join(',')}`)
    args.push(`--embed=${f.embed}`)
    return args
  },
  canSubmit: (f, { tables }) => tables.length > 0,
})
</script>

<style scoped>
.config-card { margin-bottom: 20px; }
.action-card { margin-bottom: 20px; }
</style>
