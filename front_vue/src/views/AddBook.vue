<template>
  <MyHeader></MyHeader>
  <h1> 添加图书 </h1>
  <el-row class="mb-4">
    <el-input v-model="isbn" placeholder="手动输入 ISBN "/>
  </el-row>
  <el-row class="mb-4">
    <el-button @click="add_book">手动提交</el-button>
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
    // add_book 通过 post 方法增加库存
    add_book() {
      console.log('add book isbn', this.isbn)
      let url = this.$store.state.global.baseUrl + "/"
      let param = {
        isbn:this.isbn,
        quantity:1,
        type:1,
        lib_id:4
      }

       let apiBase = axios.create({
        baseURL: url,
        withCredentials: false,
        headers: {
          'Content-Type': 'application/json;charset=UTF-8',
        },
      });

      let tmpRouter = this.$router

      apiBase.post("/library/book/add", param).then(function (response){
        console.log(response);
        if (response.data.code === 0) {
          alert("添加成功")
          tmpRouter.push({path:"/"})
        } else {
          alert("添加失败，错误码：" + response.data.code + " 错误信息：" + response.data.message)
        }
      }).catch(function (err){
        alert("Add book error "+ err)
      })
    }
  },
  created() {
    if (this.$store.state.isbn.length > 0) {
      this.isbn = this.$store.state.isbn
    }
  }
}

import {ref} from 'vue'
import axios from "axios";

const input = ref('')


</script>

<style scoped>

</style>