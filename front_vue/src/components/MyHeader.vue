<template>
  <el-menu class="row-bg" mode="horizontal" router=true>


    <el-sub-menu>
      <template #title>lib 管理</template>
      <el-menu-item index="/book/list">图书列表</el-menu-item>
      <el-menu-item index="/book/add">录入图书</el-menu-item>
      <el-menu-item index="/book/search">图书检索</el-menu-item>
    </el-sub-menu>

    <template v-if="hasLogin">
      <!-- 用户信息 -->
      <el-row>
        <el-avatar shape="square" :size="50" :src="user.avatar"/>
        <span>{{ user.username }}</span>
      </el-row>
      <el-button @click="logout">退出</el-button>
    </template>
    <template v-else>
      <el-menu-item index="/login" id="h-user">登录</el-menu-item>
    </template>
  </el-menu>
</template>


<script>

import axios from "axios";

export default {
  name: "MyHeader",
  data() {
    return {
      user: {
        username: '请先登录',
        avatar: 'https://cube.elemecdn.com/3/7c/3ea6beec64369c2642b92c6726f1epng.png'
      },
      hasLogin: false
    }
  },
  methods: {
    logout() {
      let url = this.$store.state.global.baseUrl + "/"
      let apiBase = axios.create({
        baseURL: url,
        withCredentials: false,
        headers:{'passport':localStorage.getItem('token')}
      });

      let curRouter = this.$router

      apiBase.post("/passport/logout").then(function (response) {
        console.log(response);

        if (response.data.code === 0) {
          console.log(response)
        } else {
          alert("login failed")
        }

        // 最终都需要退出
        localStorage.removeItem('token')
        localStorage.removeItem('user_name')
        curRouter.push({
          path: '/'
        });
      }).catch(function (err) {
        alert("error " + err)
      })
    }
  },
  created() {
    // 根据用户登录信息修改
    if (localStorage.getItem('token') !== null && localStorage.getItem('token') !=='') {
      this.hasLogin = true
      this.user.username = localStorage.getItem('user_name')
    }
  }
}
</script>

<style scoped>

</style>