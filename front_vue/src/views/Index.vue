<template>
  <div>
    <MyHeader></MyHeader>
    <h1>Index 页面</h1>
  </div>
</template>

<script>
import MyHeader from '../components/MyHeader'

export default {
  name: "MyIndex",
  components: {MyHeader},
  data() {
    return {
      blogs: {},
      currentPage: 1,
      total: 0,
      pageSize: 5
    }
  },
  methods: {},
  created() {
  }
}
</script>

<style scoped>

</style>