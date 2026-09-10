<template>
  <div class="home-page">
    <el-card class="welcome-card">
      <template #header>
        <div class="card-header">
          <span>欢迎使用 Sunshine Code Generator</span>
        </div>
      </template>
      <div class="welcome-content">
        <p>Sunshine 是一个代码生成工具，支持从 SQL 表结构或 Protobuf 文件快速生成 Go 微服务代码。</p>
        <el-divider />
        <h4>功能分类</h4>
        <el-descriptions :column="1" border size="small">
          <el-descriptions-item label="SQL 生成">根据数据库表结构生成 Web/gRPC 服务代码</el-descriptions-item>
          <el-descriptions-item label="Protobuf 生成">根据 .proto 文件生成 Web/gRPC 服务代码</el-descriptions-item>
          <el-descriptions-item label="独立生成"
            >单独生成 handler、service、dao、model、cache 等模块</el-descriptions-item
          >
        </el-descriptions>
        <el-divider />
        <h4>快速开始</h4>
        <el-timeline>
          <el-timeline-item timestamp="1. 选择菜单" placement="top">在左侧菜单选择需要的功能</el-timeline-item>
          <el-timeline-item timestamp="2. 填写参数" placement="top">根据页面提示填写必要参数</el-timeline-item>
          <el-timeline-item timestamp="3. 生成代码" placement="top"
            >点击「预览代码」或「生成代码」按钮</el-timeline-item
          >
        </el-timeline>
      </div>
    </el-card>
  </div>
</template>

<style scoped>
.home-page {
  max-width: 800px;
  margin: 0 auto;
}
.welcome-card {
  margin-bottom: 20px;
}
.welcome-content p {
  color: #666;
  line-height: 1.8;
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>
