<template>
  <MyHeader></MyHeader>
  <el-container>
    <el-header>
<!--      <img class="mlogo" src="https://www.markerhub.com/dist/images/logo/markerhub-logo.png" alt="">-->
      <h1>登录</h1>
    </el-header>
    <el-main>
      <el-form :model="ruleForm" :rules="rules" ref="ruleForm" label-width="100px" class="demo-ruleForm">
        <el-form-item label="用户名" prop="username">
          <el-input v-model="ruleForm.username"></el-input>
        </el-form-item>
        <el-form-item label="密码" prop="password">
          <el-input type="password" v-model="ruleForm.password"></el-input>
        </el-form-item>

        <el-form-item>
          <el-button type="primary" @click="submitForm('ruleForm')">登录</el-button>
          <el-button>重置密码</el-button>
        </el-form-item>
      </el-form>

    </el-main>
  </el-container>
</template>

<script>
import MyHeader from "@/components/MyHeader";
import axios from "axios";
import global from "@/components/Common"
import {JSEncrypt} from 'jsencrypt'

let rsa = ""

function encrypt(passwd) {
  if (rsa.length === 0 ){
    alert("get rsa public key failed")
    return
  }
  let jsEncrypt = new JSEncrypt()
  jsEncrypt.setPublicKey(rsa)
  return jsEncrypt.encrypt(passwd)
}

export default {
  name: "MyLogin",
  components: {MyHeader},

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
      config : global.config
    };
  },
  methods: {
    submitForm(formName) {
      let url = this.$store.state.global.baseUrl + "/"
      let apiBase = axios.create({
        baseURL: url,
        withCredentials: false,
      });
      let enPasswd = encrypt(this.ruleForm.password)
      console.log("rsa:", rsa, "username",this.ruleForm.username,"passwd:",enPasswd)

      let loginParam = {
        user_name: this.ruleForm.username,
        crypt_passwd: enPasswd
      }

      let curStore = this.$store
      let curRouter = this.$router

      apiBase.post("/passport/login",
        loginParam
      ).then(function (response) {
        console.log(response);
        if (response.data.code === 0) {
          console.log(response.data.data)
          localStorage.setItem("token",response.data.data.token);
          localStorage.setItem("user_name",response.data.data.user_name);

          curStore.state.global.token = response.data.data.token
          curRouter.push({
            path: '/'
          });
        } else {
          alert("login failed")
        }
      }).catch(function (err) {
        alert("error " + err)
      })
    },
    loadRsaKey(){
      let url = this.config.serverUrl + "/"
      let apiBase = axios.create({
        baseURL: url,
        withCredentials: false,
      });

      apiBase.get("/passport/rsa",{}).then(function (response) {
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
body > .el-container {
  margin-bottom: 40px;
}

.el-container:nth-child(5) .el-aside,
.el-container:nth-child(6) .el-aside {
  line-height: 260px;
}

.el-container:nth-child(7) .el-aside {
  line-height: 320px;
}

.mlogo {
  height: 60%;
  margin-top: 10px;
}

.demo-ruleForm {
  max-width: 500px;
  margin: 0 auto;
}
</style>