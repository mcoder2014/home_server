<template>
  <div>
    <MyHeader></MyHeader>
    <div class="page-container">
      <div class="card">
        <!-- 操作栏 -->
        <div class="action-bar">
          <h2 class="page-title">图书列表</h2>
          <el-button type="primary" @click="$router.push('/book/add')">
            <el-icon><Plus /></el-icon>
            添加图书
          </el-button>
        </div>

        <!-- 图书表格 -->
        <el-table :data="tableData" style="width: 100%" stripe>
          <el-table-column fixed prop="title" label="书名" min-width="120" />
          <el-table-column prop="img" label="封面" width="90">
            <template #default="scope">
              <el-image
                :src="scope.row.img || ''"
                style="max-width: 60px; max-height: 80px; border-radius: 4px;"
                fit="cover"
              >
                <template #error>
                  <img :src="bookFallback" style="max-width: 60px; max-height: 80px; border-radius: 4px; opacity: 0.7;" />
                </template>
              </el-image>
            </template>
          </el-table-column>
          <el-table-column prop="author" label="作者" min-width="80" />
          <el-table-column prop="publisher" label="出版社" min-width="100" />
          <el-table-column prop="number" label="数量" width="70" align="center" />
          <el-table-column prop="isbn" label="ISBN" min-width="130" />
          <el-table-column prop="update_time" label="更新时间" min-width="110" />
          <el-table-column fixed="right" label="操作" width="80">
            <template #default="scope">
              <el-button size="small" @click="showInfo(scope.row.info)">详情</el-button>
            </template>
          </el-table-column>
        </el-table>

        <!-- 分页 -->
        <div class="pagination-bar">
          <el-pagination
            v-model:current-page="currentPage"
            :page-size="pageSize"
            :total="totalCount"
            layout="prev, pager, next, total"
            background
            @current-change="handlePageChange"
          />
        </div>
      </div>
    </div>

    <!-- 图书详情 Dialog -->
    <el-dialog v-model="dialogVisible" title="图书详情" width="480px">
      <div v-if="selectedBook">
        <div style="text-align: center; margin-bottom: 16px;">
          <img
            :src="selectedBook.img || bookFallback"
            style="max-width: 120px; max-height: 160px; border-radius: 8px;"
            @error="$event.target.src = bookFallback"
          />
        </div>
        <el-descriptions :column="1" border>
          <el-descriptions-item label="书名">{{ selectedBook.title }}</el-descriptions-item>
          <el-descriptions-item label="作者">{{ selectedBook.author }}</el-descriptions-item>
          <el-descriptions-item label="出版社">{{ selectedBook.publisher }}</el-descriptions-item>
          <el-descriptions-item label="ISBN">{{ selectedBook.isbn }}</el-descriptions-item>
          <el-descriptions-item label="馆藏数量">{{ selectedBook.quantity }}</el-descriptions-item>
          <el-descriptions-item label="出版日期">{{ selectedBook.pubDate }}</el-descriptions-item>
          <el-descriptions-item label="更新时间">{{ selectedBook.update_time }}</el-descriptions-item>
        </el-descriptions>
      </div>
    </el-dialog>
  </div>
</template>

<script>
import MyHeader from "@/components/MyHeader";
import axios from "axios";
import {handleError} from "@/utils/handle_http_error";
import { Plus } from '@element-plus/icons-vue'
import bookFallback from '@/assets/book-fallback.svg'

export default {
  name: "BookList",
  components: { MyHeader, Plus },
  data() {
    return {
      tableData: [],
      totalCount: 0,
      pageSize: 10,
      currentPage: 1,
      dialogVisible: false,
      selectedBook: null,
      bookFallback: bookFallback,
    }
  },
  methods: {
    handlePageChange(page) {
      this.currentPage = page
      const offset = (page - 1) * this.pageSize
      this.getBookStorages(offset, this.pageSize)
    },
    updateTable(books) {
      console.log(books)
      if (books.length === 0) {
        alert("无更多图书")
      }
      this.tableData = books.map((book) => ({
        title: book.title,
        author: book.author,
        number: book.quantity,
        publisher: book.publisher,
        pubDate: book.pubDate,
        isbn: book.isbn,
        update_time: book.update_time,
        img: book.img,
        info: book,
      }))
    },
    getBookStorages(offset, limit) {
      console.log("current token:" + localStorage.getItem('token'))
      let url = this.$store.state.global.baseUrl + "/"
      let param = { offset, limit }

      let apiBase = axios.create({
        baseURL: url,
        withCredentials: false,
        headers: {'passport': localStorage.getItem('token')}
      });

      let updateTable = this.updateTable
      let refThis = this

      apiBase.get("/library/book/total", {params: param}).then(function (response) {
        console.log(response);
        handleError(response)
        if (response.data.code === 0) {
          updateTable(response.data.data.book_storages)
          refThis.totalCount = response.data.data.count
        } else {
          alert("get failed.")
        }
      }).catch(function (err) {
        alert("error " + err)
      })
    },
    showInfo(book) {
      this.selectedBook = book
      this.dialogVisible = true
    }
  },
  created() {
    this.getBookStorages(0, this.pageSize);
  }
}
</script>

<style scoped>
.pagination-bar {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
