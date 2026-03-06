<template>
  <div>
    <MyHeader></MyHeader>
    <div class="page-container">
      <div class="card form-card">
        <h2 class="page-title">录入图书</h2>
        <p class="form-desc">通过 ISBN 号码添加图书到馆藏</p>

        <el-form label-position="top" style="margin-top: 24px;">
          <el-form-item label="ISBN 号码">
            <el-input
              v-model="isbn"
              placeholder="请输入 ISBN（如：9787111111111）"
              size="large"
              :prefix-icon="Tickets"
              clearable
            />
          </el-form-item>

          <el-form-item>
            <div class="btn-group">
              <el-button type="primary" size="large" @click="add_book">
                <el-icon><Check /></el-icon>
                手动提交
              </el-button>
              <el-button size="large" @click="scanImage">
                <el-icon><Camera /></el-icon>
                相机扫码
              </el-button>
            </div>
          </el-form-item>
        </el-form>
      </div>
    </div>
  </div>
</template>


<script>
import MyHeader from "@/components/MyHeader";
import {handleError} from "@/utils/handle_http_error"
import axios from "axios";
import { Tickets, Check, Camera } from '@element-plus/icons-vue'

export default {
  name: "AddBook",
  components: { MyHeader, Tickets, Check, Camera },
  data() {
    return {
      isbn: ''
    }
  },
  setup() {
    return { Tickets, Check, Camera }
  },
  methods: {
    scanImage() {
      console.log('浏览器信息', navigator.userAgent);
      this.$router.push({ path: '/scanCodePage' });
    },
    add_book() {
      console.log('add book isbn', this.isbn)
      let url = this.$store.state.global.baseUrl + "/"
      let param = {
        isbn: this.isbn,
        quantity: 1,
        type: 1,
        lib_id: 4
      }

      let apiBase = axios.create({
        baseURL: url,
        withCredentials: false,
        headers: {
          'Content-Type': 'application/json;charset=UTF-8',
          'passport': localStorage.getItem('token')
        },
      });

      apiBase.post("/library/book/add", param).then(function (response) {
        console.log(response);
        handleError(response);
        if (response.data.code === 0) {
          alert("添加成功")
        } else {
          alert("添加失败，错误码：" + response.data.code + " 错误信息：" + response.data.message)
        }
      }).catch(function (err) {
        alert("Add book error " + err)
      })
    }
  },
  created() {
    if (this.$store.state.isbn.length > 0) {
      this.isbn = this.$store.state.isbn
    }
  }
}
</script>

<style scoped>
.form-card {
  max-width: 600px;
  margin: 0 auto;
}

.form-desc {
  margin: 0;
  color: var(--text-secondary);
  font-size: 14px;
}

.btn-group {
  display: flex;
  gap: 12px;
}
</style>
