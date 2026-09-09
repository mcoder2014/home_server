<template>
  <div>
    <MyHeader />
    <main class="page-container home-page">
      <section class="welcome-banner">
        <div class="welcome-copy">
          <span class="page-eyebrow">你的家庭工作台</span>
          <h1>欢迎回来，{{ username }}</h1>
          <p>收藏好书，记录想法，让实用的小工具随时可用。<br>你的家庭服务，从这里开始。</p>
          <router-link class="welcome-link" to="/web-projects">进入网页项目 <el-icon><ArrowRight /></el-icon></router-link>
        </div>
        <div class="home-illustration" aria-hidden="true">
          <div class="server-tile tile-back"><span></span><i></i><i></i></div>
          <div class="server-tile tile-middle"><span></span><i></i><i></i></div>
          <div class="server-tile tile-front"><b>CQ</b><div class="tile-line"></div><small>HOME SERVER</small></div>
          <div class="illustration-orbit"></div>
        </div>
      </section>

      <div class="services-heading">
        <h2>常用服务</h2>
        <span>一个入口，连接你的日常</span>
      </div>
      <div class="service-grid">
        <router-link class="service-card service-featured" to="/web-projects">
          <div class="service-top"><span class="service-icon"><el-icon :size="25"><Monitor /></el-icon></span><span class="service-label">WEB PROJECTS</span></div>
          <h3>网页项目</h3>
          <p>把文档和前端工具变成一个链接。<br>上传、发布，选择谁可以访问。</p>
          <span class="service-link">管理项目 <el-icon><ArrowRight /></el-icon></span>
        </router-link>
        <router-link class="service-card" to="/book/list">
          <div class="service-top"><span class="service-icon books-icon"><el-icon :size="25"><Reading /></el-icon></span><span class="service-label">LIBRARY</span></div>
          <h3>家庭藏书</h3>
          <p>整理纸质书与电子书，<br>让每一本好书都有迹可寻。</p>
          <span class="service-link">查看图书 <el-icon><ArrowRight /></el-icon></span>
        </router-link>
        <router-link class="service-card" to="/book/add">
          <div class="service-top"><span class="service-icon add-icon"><el-icon :size="25"><Plus /></el-icon></span><span class="service-label">QUICK ADD</span></div>
          <h3>录入图书</h3>
          <p>输入 ISBN 或扫描条码，<br>把新发现加入你的书架。</p>
          <span class="service-link">添加一本书 <el-icon><ArrowRight /></el-icon></span>
        </router-link>
      </div>
      <footer class="home-footer"><span>CQ Home Server</span><span>留给生活的一点数字空间</span></footer>
    </main>
  </div>
</template>

<script>
import MyHeader from '../components/MyHeader'
import {ArrowRight, Monitor, Plus, Reading} from '@element-plus/icons-vue'

export default {
  name: 'MyIndex',
  components: {MyHeader, Monitor, Plus, Reading, ArrowRight},
  computed: {
    username() {
      return localStorage.getItem('user_name') || '访客'
    },
  },
}
</script>

<style scoped>
.welcome-banner { position: relative; display: flex; align-items: center; justify-content: space-between; gap: 24px; overflow: hidden; padding: 46px 48px; border: 1px solid #dce6df; border-radius: 20px; background: #eaf1eb; }
.welcome-copy { position: relative; z-index: 1; }
.welcome-banner h1 { margin: 0 0 16px; font-size: clamp(26px, 3vw, 36px); font-weight: 650; letter-spacing: -1px; line-height: 1.4; overflow-wrap: anywhere; }
.welcome-banner p { color: #526c62; font-size: 15px; line-height: 1.9; margin: 0 0 24px; }
.welcome-link { display: inline-flex; gap: 10px; align-items: center; color: var(--primary-darker); font-size: 14px; font-weight: 650; text-decoration: none; border-bottom: 1px solid #92b5a6; padding-bottom: 5px; }
.home-illustration { position: relative; width: 260px; height: 224px; flex-shrink: 0; }
.server-tile { position: absolute; width: 176px; height: 114px; border: 1px solid #b9cdc0; border-radius: 16px; transform: rotate(-12deg) skew(8deg); box-shadow: 10px 20px 30px #27493210; }
.tile-back { top: 13px; left: 48px; background: #d0ddd0; }
.tile-middle { top: 51px; left: 37px; background: #dde8dc; }
.server-tile span { display: inline-block; width: 62px; height: 5px; margin: 20px 20px 0; background: #9ab8a5; border-radius: 4px; }
.server-tile i { display: inline-block; width: 5px; height: 5px; margin-right: 7px; border-radius: 50%; background: #90a997; }
.tile-front { top: 90px; left: 26px; padding: 16px 22px; background: #fffef7; border-color: #ccd9cb; }
.tile-front b { font-size: 32px; font-weight: 600; letter-spacing: -2px; color: #38594a; }
.tile-front small { font-size: 8px; letter-spacing: 2px; color: #648371; }
.tile-line { width: 100%; height: 1px; background: #dae4d7; margin: 3px 0 7px; }
.illustration-orbit { position: absolute; width: 260px; height: 260px; border: 1px solid #d1dfd3; border-radius: 50%; top: -8px; left: -8px; z-index: -1; }
.services-heading { display: flex; align-items: center; justify-content: space-between; margin: 36px 0 18px; gap: 12px; }
.services-heading h2 { font-size: 18px; font-weight: 650; margin: 0; }
.services-heading > span { color: var(--text-secondary); font-size: 12px; }
.service-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 20px; }
.service-card { display: flex; flex-direction: column; text-decoration: none; padding: 28px; border: 1px solid var(--border-color); border-radius: 16px; background: #fff; color: var(--text-primary); box-shadow: var(--card-shadow); transition: transform 0.2s, box-shadow 0.2s, border-color 0.2s; }
.service-card:hover { transform: translateY(-3px); border-color: #b4cdc0; box-shadow: 0 12px 28px #1d352d0a; }
.service-top { display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-bottom: 22px; }
.service-icon { display: grid; place-items: center; width: 48px; height: 48px; border-radius: 13px; background: #e9f3ed; color: var(--primary-color); }
.books-icon { background: #f7efe1; color: #aa8244; }
.add-icon { background: #eff1f8; color: #7b83ab; }
.service-label { color: #7f8d87; font-size: 9px; font-weight: 600; letter-spacing: 1.1px; }
.service-card h3 { font-size: 20px; font-weight: 650; margin: 0 0 10px; }
.service-card p { font-size: 13px; line-height: 1.9; color: var(--text-secondary); margin: 0 0 26px; }
.service-link { display: flex; align-items: center; justify-content: space-between; font-size: 13px; font-weight: 550; margin-top: auto; }
.service-featured { background: #213e37; border-color: #213e37; color: #fff; }
.service-featured .service-icon { background: #ffffff12; color: #c7dfcf; }
.service-featured .service-label { color: #a6bdb0; }
.service-featured p { color: #bccdc3; }
.service-featured .service-link { color: #dbeade; }
.home-footer { display: flex; justify-content: space-between; gap: 12px; color: #79877f; font-size: 11px; margin-top: 38px; padding-top: 20px; border-top: 1px solid var(--border-color); }
@media (max-width: 860px) {
  .welcome-banner { padding: 34px; }
  .home-illustration { width: 210px; transform: scale(0.85); transform-origin: right center; }
  .service-card { padding: 22px; }
  .service-label { display: none; }
}
@media (max-width: 640px) {
  .welcome-banner { padding: 28px 24px; }
  .home-illustration { display: none; }
  .welcome-banner p { font-size: 14px; }
  .service-grid { grid-template-columns: 1fr; gap: 14px; }
  .service-top { margin-bottom: 14px; }
  .service-label { display: inline; }
  .service-card p { margin-bottom: 20px; }
  .services-heading > span { display: none; }
}
</style>
