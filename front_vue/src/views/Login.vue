<template>
  <div class="login-page">
    <div class="login-card">
      <!-- Logo + 标题 -->
      <div class="login-header">
        <el-icon :size="40" color="#409eff"><Reading /></el-icon>
        <h2 class="login-title">图书管理系统</h2>
        <p class="login-subtitle">请登录您的账号</p>
      </div>

      <el-form
        :model="ruleForm"
        :rules="rules"
        ref="ruleForm"
        label-position="top"
        class="login-form"
      >
        <el-form-item label="用户名" prop="username">
          <el-input
            v-model="ruleForm.username"
            placeholder="请输入用户名"
            :prefix-icon="User"
            size="large"
          />
        </el-form-item>
        <el-form-item label="密码" prop="password">
          <el-input
            type="password"
            v-model="ruleForm.password"
            placeholder="请输入密码"
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
            @click="submitForm('ruleForm')"
          >
            登 录
          </el-button>
        </el-form-item>
      </el-form>
    </div>
  </div>
</template>

<script>
import axios from "axios";
import global from "@/components/Common"
import {JSEncrypt} from 'jsencrypt'
import { Reading, User, Lock } from '@element-plus/icons-vue'

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
  components: { Reading, User, Lock },

  data() {
    return {
      ruleForm: {
        username: 'admin',
        password: '123456'
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
      rsa: "rsa",
      config: global.config
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
      console.log("rsa:", rsa, "username", this.ruleForm.username, "passwd:", enPasswd)

      let loginParam = {
        user_name: this.ruleForm.username,
        crypt_passwd: enPasswd
      }

      let curStore = this.$store
      let curRouter = this.$router

      apiBase.post("/passport/login", loginParam).then(function (response) {
        console.log(response);
        if (response.data.code === 0) {
          console.log(response.data.data)
          localStorage.setItem("token", response.data.data.token);
          localStorage.setItem("user_name", response.data.data.user_name);

          curStore.state.global.token = response.data.data.token
          curRouter.push({ path: '/' });
        } else {
          alert("login failed")
        }
      }).catch(function (err) {
        alert("error " + err)
      })
    },
    loadRsaKey() {
      let url = this.config.serverUrl + "/"
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
.login-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #1a73e8, #0d47a1);
  padding: 20px;
}

.login-card {
  background: #ffffff;
  border-radius: 12px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.2);
  padding: 40px 36px;
  width: 100%;
  max-width: 400px;
}

.login-header {
  text-align: center;
  margin-bottom: 32px;
}

.login-title {
  margin: 12px 0 4px 0;
  font-size: 22px;
  font-weight: 700;
  color: #303133;
}

.login-subtitle {
  margin: 0;
  font-size: 14px;
  color: #909399;
}

.login-form :deep(.el-form-item__label) {
  font-weight: 500;
}

.login-btn {
  width: 100%;
  font-size: 16px;
  letter-spacing: 4px;
}
</style>
