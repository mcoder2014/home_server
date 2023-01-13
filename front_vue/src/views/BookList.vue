<template>
  <MyHeader></MyHeader>

  <!-- 图书表格 -->
  <el-main>
    <el-row>
      <el-table :data="tableData" style="width: 100%">
        <el-table-column fixed prop="title" label="书名"/>
        <el-table-column prop="img" label="封面">
          <template #default="scope">
            <div v-if="scope.row.img.length !== 0">
              <div style="display: flex; align-items: center">
                <el-image :src="scope.row.img"/>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="author" label="作者"/>
        <el-table-column prop="publisher" label="出版社"/>
        <el-table-column prop="number" label="保有数量"></el-table-column>
        <!--        <el-table-column prop="pubDate" label="出版时间"/>-->
        <el-table-column prop="isbn" label="ISBN"/>
        <el-table-column prop="update_time" label="更新时间"/>
        <el-table-column fixed="right" label="操作">
          <template #default="scope">
            <el-button @click="showInfo(scope.row.info)">
              详情
            </el-button>
            <!--            <div style="display: flex; align-items: center">-->
            <!--              <el-image :src="scope.row.img"/>-->
            <!--            </div>-->
          </template>
        </el-table-column>
      </el-table>
    </el-row>


    <!-- 翻页及 添加图书-->
    <el-row>
      <el-button class="mt-4" @click="scanImage">Add Item</el-button>
      <div v-for="b in pageButton">
        <el-button class="mt-4" @click="getBookStorages(b.offset, b.limit)">{{ b.pageNum }}</el-button>
      </div>

    </el-row>


  </el-main>
</template>

<script>
import MyHeader from "@/components/MyHeader";
import axios from "axios";
import {ElMessageBox} from "element-plus";
import {handleError} from "@/utils/handle_http_error";

export default {
  name: "BookList",
  components: {MyHeader},
  data() {
    return {
      tableData: [
        {
          title: '测试图书',
          author: '超群',
          number: 1,
          publisher: '超群出版社',
          pubDate: '2000-01-01',
          isbn: '`1234567890123',
          img: 'https://img.maimiaobook.com/cover/5608VP471D.jpg?x-oss-process=style/yuantu',
          update_time: '2000-01-01',
          info: {}
        }
      ],
      // 总数目
      totalCount: 10,
      // 页大小
      pageSize: 10,
      pageButton: [
        {
          offset: 0,
          limit: 10,
          pageNum: 1,
        }
      ]
    }
  },
  methods: {
    scanImage() {
      console.log('浏览器信息', navigator.userAgent);
      this.$router.push({
        path: '/book/add'
      });
    },
    updateTable(books) {
      console.log(books)
      if (books.length === 0) {
        alert("无更多图书")
      }
      this.tableData = [];
      books.forEach((book) => {
        this.tableData.push({
          title: book.title,
          author: book.author,
          number: book.quantity,
          publisher: book.publisher,
          pubDate: book.pubDate,
          isbn: book.isbn,
          update_time: book.update_time,
          img: book.img,
          info: book,
        });
      });
    },
    updatePageButton(count) {
      this.totalCount = count

      let pageSize = this.pageSize
      let pages = Math.floor((count + pageSize - 1) / pageSize)
      console.log(pages)
      this.pageButton = []
      for (let i = 0; i < pages; i++) {
        this.pageButton.push({
          offset: i * pageSize,
          limit: pageSize,
          pageNum: i + 1,
        })
      }

    },
    getBookStorages(offset, limit) {
      console.log("current token:"+localStorage.getItem('token') )
      let url = this.$store.state.global.baseUrl + "/"
      let param = {
        offset: offset,
        limit: limit,
      }

      let apiBase = axios.create({
        baseURL: url,
        withCredentials: false,
        headers:{'passport':localStorage.getItem('token')}
      });

      let updateTable = this.updateTable
      let refThis = this

      apiBase.get("/library/book/total", {
        params: param,
      }).then(function (response) {
        console.log(response);
        handleError(response)
        if (response.data.code === 0) {
          console.log(response.data.data)
          // 更新表格
          updateTable(response.data.data.book_storages)
          refThis.updatePageButton(response.data.data.count)

        } else {
          alert("get failed.")
        }
      }).catch(function (err) {
        alert("error " + err)
      })
    },
    showInfo(book) {
      ElMessageBox.alert(book, '详情', {
        confirmButtonText: 'OK',
        callback: (action) => {
        },
      })
    }
  },
  created() {
    this.getBookStorages(0, 10);
  }
}
</script>

<style scoped>

</style>