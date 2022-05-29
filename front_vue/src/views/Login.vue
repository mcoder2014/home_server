<template>
  <MyHeader></MyHeader>
  <el-container>
    <el-header>
      <img class="mlogo" src="https://www.markerhub.com/dist/images/logo/markerhub-logo.png" alt="">
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

export default {
  name: "MyLogin",
  components: {MyHeader},

  data() {
    return {
      ruleForm: {
        username: 'markerhub',
        password: '111111'
      },
      rules: {
        username: [
          {required: true, message: '请输入用户名', trigger: 'blur'},
          {min: 3, max: 15, message: '长度在 3 到 15 个字符', trigger: 'blur'}
        ],
        password: [
          {required: true, message: '请选择密码', trigger: 'change'}
        ]
      }
    };
  },
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