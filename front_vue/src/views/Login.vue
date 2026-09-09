<template>
  <div class="login-page">
    <router-link to="/" class="login-brand"><span class="brand-mark" aria-hidden="true">CQ</span>CQ Home Server</router-link>
    <main class="login-layout">
      <section class="login-intro">
        <span class="page-eyebrow">一个属于自己的数字空间</span>
        <h1>生活里的好想法，<br>在这里安放。</h1>
        <p>管理家庭藏书，发布实用网页。<br>用同一个账号，连接你的每一份收藏与创造。</p>
        <div class="intro-note"><span></span>你的内容，由你决定与谁分享</div>
      </section>
      <div class="login-card">
      <div class="login-header">
        <h2 class="login-title">登录你的空间</h2>
        <p class="login-subtitle">欢迎回来，请输入账号信息。</p>
      </div>

      <el-form
        :model="ruleForm"
        :rules="rules"
        ref="ruleForm"
        label-position="top"
        class="login-form"
        @submit.prevent="submitForm('ruleForm')"
      >
        <el-form-item label="用户名" prop="username">
          <el-input
            v-model="ruleForm.username"
            placeholder="请输入用户名"
            autocomplete="username"
            :prefix-icon="User"
            size="large"
          />
        </el-form-item>
        <el-form-item label="密码" prop="password">
          <el-input
            type="password"
            v-model="ruleForm.password"
            placeholder="请输入密码"
            autocomplete="current-password"
            :prefix-icon="Lock"
            show-password
            size="large"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            size="large"
            class="login-btn"
            native-type="submit"
          >
            登录
          </el-button>
        </el-form-item>
      </el-form>
      <router-link to="/" class="back-home">返回首页</router-link>
      </div>
    </main>
    <footer class="login-footer">CQ Home Server · 留给生活的一点数字空间</footer>
  </div>
</template>

<script>
import axios from "axios";
import {JSEncrypt} from 'jsencrypt'
import { User, Lock } from '@element-plus/icons-vue'

const {synchronizeBrowserIdentity, webProjectsApi} = require('@/api/web_projects.cjs')
const {isSafeProjectTarget, normalizeInternalRedirect} = require('@/utils/web_projects_navigation.cjs')

let rsa = ""

function encrypt(passwd) {
  if (rsa.length === 0) {
    alert("get rsa public key failed")
    return
  }
  let jsEncrypt = new JSEncrypt()
  jsEncrypt.setPublicKey(rsa)
  return jsEncrypt.encrypt(passwd)
}

export default {
  name: "MyLogin",
  components: { User, Lock },

  data() {
    return {
      ruleForm: {
        username: '',
        password: ''
      },
      rules: {
        username: [
          {required: true, message: '请输入用户名', trigger: 'blur'},
          {min: 3, max: 15, message: '长度在 3 到 15 个字符', trigger: 'blur'}
        ],
        password: [
          {required: true, message: '请选择密码', trigger: 'change'}
        ]
      },
      rsa: "rsa"
    };
  },
  setup() {
    return { User, Lock }
  },
  methods: {
    submitForm(formName) {
      let url = this.$store.state.global.baseUrl + "/"
      let apiBase = axios.create({
        baseURL: url,
        withCredentials: false,
      });
      let enPasswd = encrypt(this.ruleForm.password)
      let loginParam = {
        user_name: this.ruleForm.username,
        crypt_passwd: enPasswd
      }

      let curStore = this.$store
      let curRouter = this.$router
      const redirect = normalizeInternalRedirect(this.$route.query.redirect)

      apiBase.post("/passport/login", loginParam).then(async function (response) {
        if (response.data.code === 0) {
          const browserLoginAvailable = await synchronizeBrowserIdentity(webProjectsApi, response.data.data, (identity) => {
            curStore.commit('SET_TOKEN', identity.token)
            localStorage.setItem("user_name", identity.user_name)
          })

          if (isSafeProjectTarget(redirect)) {
            if (!browserLoginAvailable) {
              curRouter.push('/')
              return
            }
            window.location.replace(redirect)
            return
          }
          curRouter.push(redirect)
        } else {
          alert("login failed")
        }
      }).catch(function (err) {
        alert("error " + err)
      })
    },
    loadRsaKey() {
      let url = this.$store.state.global.baseUrl + "/"
      let apiBase = axios.create({
        baseURL: url,
        withCredentials: false,
      });

      apiBase.get("/passport/rsa", {}).then(function (response) {
        console.log(response);
        if (response.data.code === 0) {
          console.log(response.data.data)
          rsa = response.data.data
        } else {
          alert("get failed.")
        }
      }).catch(function (err) {
        alert("error " + err)
      })
    }
  },
  created() {
    this.loadRsaKey()
  }
}
</script>

<style scoped>
.login-page { min-height: 100vh; display: flex; flex-direction: column; padding: 32px 48px 24px; background: radial-gradient(ellipse at 12% 46%, #e3eee2 0%, transparent 58%), #f5f6f1; }
.login-brand { display: inline-flex; gap: 12px; align-items: center; align-self: flex-start; color: var(--text-primary); text-decoration: none; font-size: 17px; font-weight: 700; letter-spacing: -0.4px; }
.login-layout { display: grid; grid-template-columns: 1fr 400px; align-items: center; gap: 80px; max-width: 1000px; width: 100%; flex: 1; margin: 50px auto; }
.login-intro h1 { font-size: clamp(30px, 4vw, 44px); font-weight: 650; letter-spacing: -1px; line-height: 1.5; margin: 18px 0 22px; }
.login-intro p { color: var(--text-secondary); font-size: 15px; line-height: 2; }
.intro-note { display: flex; align-items: center; gap: 10px; font-size: 12px; color: #6c806e; margin-top: 42px; }
.intro-note span { height: 1px; width: 28px; background: #9bb49b; }
.login-card { background: #fff; border: 1px solid #e3e9df; border-radius: 20px; box-shadow: 0 18px 60px #203a2b08; padding: 38px 36px 28px; width: 100%; }
.login-header { margin-bottom: 30px; }
.login-title { margin: 0 0 8px; font-size: 24px; font-weight: 650; }
.login-subtitle { margin: 0; font-size: 13px; color: var(--text-secondary); }
.login-form :deep(.el-form-item) { margin-bottom: 24px; }
.login-form :deep(.el-input__wrapper) { min-height: 46px; }
.login-btn { width: 100%; height: 46px; font-size: 15px; margin-top: 6px; }
.back-home { display: block; text-align: center; color: var(--text-secondary); font-size: 12px; text-decoration: none; }
.back-home:hover { color: var(--primary-color); }
.login-footer { text-align: center; color: #7c8a80; font-size: 11px; }
@media (max-width: 820px) {
  .login-page { padding: 24px; }
  .login-layout { grid-template-columns: 1fr; max-width: 420px; gap: 26px; margin: 40px auto; }
  .login-intro h1 { font-size: 28px; margin: 12px 0; }
  .login-intro p, .intro-note { display: none; }
  .login-card { padding: 30px 26px; }
}
</style>
