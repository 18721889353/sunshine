<template>
  <div class="app-container">
    <el-container style="height: 100vh;">
      <!-- 左侧菜单 -->
      <el-aside width="220px" class="app-aside">
        <div class="aside-header">
          <span class="logo-icon">☀️</span>
          <span class="logo-text">Sunshine</span>
        </div>
        <el-menu :default-active="activeMenu" class="aside-menu" @select="handleMenuSelect">
          <el-menu-item index="home">
            <el-icon><HomeFilled /></el-icon>
            <span>首页</span>
          </el-menu-item>

          <el-sub-menu index="sql">
            <template #title>
              <el-icon><Coin /></el-icon>
              <span>SQL 生成</span>
            </template>
            <el-menu-item index="sql-http">创建 Web 服务</el-menu-item>
            <el-menu-item index="sql-rpc">创建 gRPC 服务</el-menu-item>
            <el-menu-item index="sql-handler-pb">创建 Handler+Protobuf</el-menu-item>
          </el-sub-menu>

          <el-sub-menu index="proto">
            <template #title>
              <el-icon><Document /></el-icon>
              <span>Protobuf 生成</span>
            </template>
            <el-menu-item index="proto-http">创建 Web 服务</el-menu-item>
            <el-menu-item index="proto-rpc-gw">创建 gRPC 网关</el-menu-item>
            <el-menu-item index="proto-grpc-http">创建 gRPC+HTTP</el-menu-item>
          </el-sub-menu>

          <el-sub-menu index="gen">
            <template #title>
              <el-icon><FolderOpened /></el-icon>
              <span>独立生成</span>
            </template>
            <el-menu-item index="gen-handler">Handler CRUD</el-menu-item>
            <el-menu-item index="gen-service">Service</el-menu-item>
            <el-menu-item index="gen-service-handler">Service+Handler</el-menu-item>
            <el-menu-item index="gen-dao">DAO CRUD</el-menu-item>
            <el-menu-item index="gen-proto">Protobuf CRUD</el-menu-item>
            <el-menu-item index="gen-model">Model</el-menu-item>
            <el-menu-item index="gen-cache">Cache</el-menu-item>
            <el-menu-item index="gen-rpc-conn">gRPC 连接</el-menu-item>
            <el-menu-item index="gen-config">Config (YAML→Go)</el-menu-item>
            <el-menu-item index="gen-graph">业务架构图</el-menu-item>
          </el-sub-menu>
        </el-menu>
      </el-aside>

      <!-- 右侧内容 -->
      <el-container>
        <el-header class="app-header">
          <h2>{{ pageTitle }}</h2>
        </el-header>
        <el-main class="app-main">
          <Home v-if="activeMenu === 'home'" />
          <SqlHttp v-else-if="activeMenu === 'sql-http'" />
          <SqlRpc v-else-if="activeMenu === 'sql-rpc'" />
          <SqlHandlerPb v-else-if="activeMenu === 'sql-handler-pb'" />
          <ProtoHttp v-else-if="activeMenu === 'proto-http'" />
          <ProtoRpcGw v-else-if="activeMenu === 'proto-rpc-gw'" />
          <ProtoGrpcHttp v-else-if="activeMenu === 'proto-grpc-http'" />
          <GenHandler v-else-if="activeMenu === 'gen-handler'" />
          <GenService v-else-if="activeMenu === 'gen-service'" />
          <GenServiceHandler v-else-if="activeMenu === 'gen-service-handler'" />
          <GenDao v-else-if="activeMenu === 'gen-dao'" />
          <GenProto v-else-if="activeMenu === 'gen-proto'" />
          <GenModel v-else-if="activeMenu === 'gen-model'" />
          <GenCache v-else-if="activeMenu === 'gen-cache'" />
          <GenRpcConn v-else-if="activeMenu === 'gen-rpc-conn'" />
          <GenConfig v-else-if="activeMenu === 'gen-config'" />
          <GenGraph v-else-if="activeMenu === 'gen-graph'" />
        </el-main>
      </el-container>
    </el-container>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { HomeFilled, Coin, Document, FolderOpened } from '@element-plus/icons-vue'

import Home from './pages/home/index.vue'
import SqlHttp from './pages/sql-http/index.vue'
import SqlRpc from './pages/sql-rpc/index.vue'
import SqlHandlerPb from './pages/sql-handler-pb/index.vue'
import ProtoHttp from './pages/proto-http/index.vue'
import ProtoRpcGw from './pages/proto-rpc-gw/index.vue'
import ProtoGrpcHttp from './pages/proto-grpc-http/index.vue'
import GenHandler from './pages/gen-handler/index.vue'
import GenService from './pages/gen-service/index.vue'
import GenServiceHandler from './pages/gen-service-handler/index.vue'
import GenDao from './pages/gen-dao/index.vue'
import GenProto from './pages/gen-proto/index.vue'
import GenModel from './pages/gen-model/index.vue'
import GenCache from './pages/gen-cache/index.vue'
import GenRpcConn from './pages/gen-rpc-conn/index.vue'
import GenConfig from './pages/gen-config/index.vue'
import GenGraph from './pages/gen-graph/index.vue'

const activeMenu = ref('home')

const handleMenuSelect = (index) => {
  activeMenu.value = index
}

const pageTitle = computed(() => {
  const titles = {
    'home': '首页',
    'sql-http': 'SQL → Web 服务',
    'sql-rpc': 'SQL → gRPC 服务',
    'sql-handler-pb': 'SQL → Handler + Protobuf',
    'proto-http': 'Protobuf → Web 服务',
    'proto-rpc-gw': 'Protobuf → gRPC 网关',
    'proto-grpc-http': 'Protobuf → gRPC + HTTP',
    'gen-handler': '独立生成 → Handler CRUD',
    'gen-service': '独立生成 → Service',
    'gen-service-handler': '独立生成 → Service + Handler',
    'gen-dao': '独立生成 → DAO CRUD',
    'gen-proto': '独立生成 → Protobuf CRUD',
    'gen-model': '独立生成 → Model',
    'gen-cache': '独立生成 → Cache',
    'gen-rpc-conn': '独立生成 → gRPC 连接',
    'gen-config': '独立生成 → Config (YAML→Go)',
    'gen-graph': '独立生成 → 业务架构图',
  }
  return titles[activeMenu.value] || 'Sunshine Code Generator'
})
</script>

<style scoped>
.app-container { height: 100vh; }
.app-aside { background-color: #fff; border-right: 1px solid #e6e6e6; overflow-y: auto; }
.aside-header { height: 60px; display: flex; align-items: center; justify-content: center; gap: 8px; border-bottom: 1px solid #e6e6e6; }
.logo-icon { font-size: 24px; }
.logo-text { font-size: 18px; font-weight: bold; color: #409eff; }
.aside-menu { border-right: none; }
.app-header { background-color: #fff; border-bottom: 1px solid #e6e6e6; display: flex; align-items: center; padding: 0 20px; }
.app-header h2 { margin: 0; font-size: 18px; color: #333; }
.app-main { background-color: #f5f7fa; padding: 20px; overflow-y: auto; }
</style>
