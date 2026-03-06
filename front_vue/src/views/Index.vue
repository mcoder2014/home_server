<template>
  <div>
    <MyHeader></MyHeader>
    <div class="page-container">
      <!-- 欢迎横幅 -->
      <div class="welcome-banner">
        <h1>欢迎回来，{{ username }}</h1>
        <p>在这里管理您的图书馆藏书</p>
      </div>

      <!-- 功能快捷卡片 -->
      <el-row :gutter="16">
        <el-col :xs="24" :sm="12">
          <div class="quick-card" @click="$router.push('/book/list')">
            <div class="quick-card-icon">
              <el-icon :size="36" color="#409eff"><List /></el-icon>
            </div>
            <div class="quick-card-body">
              <h3>图书列表</h3>
              <p>查看和管理所有馆藏图书</p>
            </div>
            <el-icon class="quick-card-arrow"><ArrowRight /></el-icon>
          </div>
        </el-col>
        <el-col :xs="24" :sm="12">
          <div class="quick-card" @click="$router.push('/book/add')">
            <div class="quick-card-icon">
              <el-icon :size="36" color="#67c23a"><Plus /></el-icon>
            </div>
            <div class="quick-card-body">
              <h3>录入图书</h3>
              <p>通过 ISBN 或扫码添加新图书</p>
            </div>
            <el-icon class="quick-card-arrow"><ArrowRight /></el-icon>
          </div>
        </el-col>
      </el-row>
    </div>
  </div>
</template>

<script>
import MyHeader from '../components/MyHeader'
import { List, Plus, ArrowRight } from '@element-plus/icons-vue'

export default {
  name: "MyIndex",
  components: { MyHeader, List, Plus, ArrowRight },
  computed: {
    username() {
      return localStorage.getItem('user_name') || '访客'
    }
  }
}
</script>

<style scoped>
.quick-card {
  display: flex;
  align-items: center;
  gap: 16px;
  background: #ffffff;
  border-radius: 8px;
  box-shadow: var(--card-shadow);
  padding: 20px;
  cursor: pointer;
  transition: box-shadow 0.2s, transform 0.2s;
  margin-bottom: 16px;
}

.quick-card:hover {
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.15);
  transform: translateY(-2px);
}

.quick-card-icon {
  flex-shrink: 0;
}

.quick-card-body {
  flex: 1;
}

.quick-card-body h3 {
  margin: 0 0 4px 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
}

.quick-card-body p {
  margin: 0;
  font-size: 13px;
  color: var(--text-secondary);
}

.quick-card-arrow {
  color: #c0c4cc;
  flex-shrink: 0;
}
</style>
