<template>
  <MyHeader></MyHeader>
  <h1> 添加图书 </h1>
  <el-row class="mb-4">
    <el-input v-model="isbn" placeholder="手动输入 ISBN "/>
  </el-row>
  <el-row class="mb-4">
    <el-button>手动提交</el-button>
    <el-button @click="scanImage">相机扫描</el-button>
  </el-row>
</template>


<script>

import MyHeader from "@/components/MyHeader";

export default {
  name: "AddBook",
  components: {MyHeader},
  data() {
    return {
      isbn: ''
    }
  },
  methods: {
    scanImage() {
      console.log('浏览器信息', navigator.userAgent);
      this.$router.push({
        path: '/scanCodePage'
      });
    },
    add_book() {
      console.log('add book isbn', this.isbn)
    }
  },
  created() {
    if (this.$store.state.isbn.length > 0) {
      this.isbn = this.$store.state.isbn
    }
  }
}

import {ref} from 'vue'

const input = ref('')


</script>

<style scoped>

</style>