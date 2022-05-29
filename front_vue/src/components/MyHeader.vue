<template>
  <el-menu class="row-bg" mode="horizontal" router=true>
    <el-menu-item index="/login" id="h-user"> login</el-menu-item>
    <el-sub-menu>
      <template #title>lib 管理</template>
      <el-menu-item index="/book/list">图书列表</el-menu-item>
      <el-menu-item index="/book/add">录入图书</el-menu-item>
      <el-menu-item index="/book/search">图书检索</el-menu-item>
    </el-sub-menu>
    <el-menu-item @click="logout">Logout</el-menu-item>
  </el-menu>
</template>


<script>

export default {
  name: "MyHeader",
  data() {
    console.log("header")
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
      // const _this = this
      // _this.$axios.get("/logout", {
      //   headers: {
      //     "Authorization": localStorage.getItem("token")
      //   }
      // }).then(res => {
      //   _this.$store.commit("REMOVE_INFO")
      //   _this.$router.push("/login")
      //
      // })
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