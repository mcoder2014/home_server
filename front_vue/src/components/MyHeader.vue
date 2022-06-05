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
      <el-menu-item @click="logout">退出</el-menu-item>
    </template>
    <template v-else>
      <el-menu-item index="/login" id="h-user">登录</el-menu-item>
    </template>


  </el-menu>
</template>


<script>

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
      const _this = this
      _this.$axios.get("/logout", {
        headers: {
          "Authorization": localStorage.getItem("token")
        }
      }).then(res => {
        _this.$store.commit("REMOVE_INFO")
        _this.$router.push("/login")

      })
    }
  },
  created() {
    // 根据用户登录信息修改
    // if(this.$store.getters.getUser.username) {
    //   this.user.username = this.$store.getters.getUser.username
    //   this.user.avatar = this.$store.getters.getUser.avatar
    //
    //   this.hasLogin = true
    // }

  }
}
</script>

<style scoped>

</style>