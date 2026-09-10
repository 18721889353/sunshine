<template>
  <div class="generate-page">
    <el-card class="main-card">
      <template #header>
        <span class="card-title"
          >④基于<el-text type="danger">protobuf</el-text>创建grpc服务
          <el-text type="info" size="small">生成通用的微服务项目代码</el-text></span
        >
      </template>

      <el-form label-width="120px" label-position="right">
        <!-- proto文件 -->
        <el-form-item label="proto文件" required>
          <div style="display: flex; align-items: center; gap: 16px">
            <el-button type="primary" link @click="showProtoExample = true">查看protobuf文件示例</el-button>
          </div>
          <el-upload
            class="upload-demo"
            drag
            :auto-upload="false"
            :on-change="handleFileChange"
            :on-remove="handleFileRemove"
            accept=".proto"
            multiple
            style="width: 100%; margin-top: 8px"
          >
            <el-icon class="el-icon--upload"><upload-filled /></el-icon>
            <div class="el-upload__text">选择proto文件，支持多文件。</div>
          </el-upload>
          <div v-if="protoFiles.length" style="margin-top: 8px; color: #909399; font-size: 12px">
            已选 {{ protoFiles.length }} 个文件
          </div>
        </el-form-item>

        <!-- 服务名称 -->
        <el-form-item label="服务名称" required>
          <el-input v-model="form.serverName" placeholder="服务名称" />
        </el-form-item>

        <!-- module名称 -->
        <el-form-item label="module名称" required>
          <el-input v-model="form.moduleName" placeholder="go.mod 中的 module 名称" />
        </el-form-item>

        <!-- 项目名称 -->
        <el-form-item label="项目名称" required>
          <el-input v-model="form.projectName" placeholder="用于部署名称" />
        </el-form-item>

        <!-- docker镜像仓库 -->
        <el-form-item label="docker镜像仓库">
          <el-input v-model="form.repoAddr" placeholder="Docker 镜像仓库地址（可选）" />
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

    <!-- protobuf文件示例弹窗 -->
    <el-dialog v-model="showProtoExample" title="user.proto" width="900px">
      <pre
        style="
          margin: 0;
          white-space: pre-wrap;
          word-break: break-all;
          font-size: 13px;
          background: #f5f7fa;
          padding: 16px;
          border-radius: 4px;
        "
      >
syntax = "proto3";

package api.user.v1;

// 第一个user是go module名称, 第二个user是服务名称, v1是版本号
option go_package = "user/api/user/v1;v1";

service user {
  // Login 登录注释
  rpc Login(LoginRequest) returns (LoginReply) {}

  rpc ClientSteam(stream HelloRequest) returns (HelloReply) {}
  rpc ServerSteam(HelloRequest) returns (stream HelloReply) {}
  rpc BidiSteam(stream HelloRequest) returns (stream HelloReply) {}
}

message LoginRequest {
  string email = 1;
  string password = 2;
}

message LoginReply {
  string token = 1;
}

message HelloRequest {
  string name = 1;
}

message HelloReply {
  string msg = 1;
}</pre>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { UploadFilled } from '@element-plus/icons-vue'
import { useCodeGenerator } from '../../composables/useCodeGenerator.js'

const showProtoExample = ref(false)

const { form, protoFiles, previewResult, loading, canGenerate, previewCode, generateCode, copyCommand } =
  useCodeGenerator({
    command: 'micro rpc-pb',
    buildArgs: (f, { protoFiles }) => {
      const args = []
      if (f.moduleName) args.push(`--module-name=${f.moduleName}`)
      if (f.serverName) args.push(`--server-name=${f.serverName}`)
      if (f.projectName) args.push(`--project-name=${f.projectName}`)
      if (f.repoAddr) args.push(`--repo-addr=${f.repoAddr}`)
      if (f.outPath) args.push(`--out=${f.outPath}`)
      if (protoFiles.length) args.push(`--protobuf-file=${protoFiles.map((p) => p.name).join(',')}`)
      args.push('--suited-mono-repo=false')
      return args
    },
    canSubmit: (f, { protoFiles }) => protoFiles.length > 0,
  })

const fileList = []
const handleFileChange = (file, newFileList) => {
  fileList.length = 0
  newFileList.forEach((f) => fileList.push(f.raw))
  protoFiles.value = [...fileList]
}
const handleFileRemove = (file, newFileList) => {
  fileList.length = 0
  newFileList.forEach((f) => fileList.push(f.raw))
  protoFiles.value = [...fileList]
}
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
