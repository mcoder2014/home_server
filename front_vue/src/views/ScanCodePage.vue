<template>
  <div class="page-scan">
    <h1>扫描图书条形码</h1>
    <!-- 扫码区域 -->
    <video ref="video" id="video" class="scan-video" autoplay></video>
    <!-- 提示语 -->
    <div v-show="tipShow"> {{ tipMsg }}</div>
  </div>
</template>

<script>
import {BrowserMultiFormatReader} from '@zxing/library';

export default {
  name: 'scanCodePage',
  data() {
    return {
      loadingShow: false,
      codeReader: null,
      scanText: '',
      vin: null,
      tipMsg: '正在尝试识别....',
      tipShow: false,
    }
  },
  created() {
    this.codeReader = new BrowserMultiFormatReader();
    this.openScan();
  },
  destroyed() {

  },
  watch: {
    '$route'(to, from) {
      if (to.path === '/scanCodePage') {
        this.codeReader = new BrowserMultiFormatReader();
        this.openScan();
      }
    }
  },
  methods: {
    // 枚举摄像头并按现有设备顺序和后置标签选择输入；无设备时返回上一页，调用失败时更新提示状态。
    async openScan() {
      // 取得设备后展示调用提示；首个设备标记为 back 时使用它，多设备且首个无该标签时选择第二个。
      this.codeReader.listVideoInputDevices().then((videoInputDevices) => {
        this.tipShow = true;
        this.tipMsg = '正在调用摄像头...';
        console.log('videoInputDevices', videoInputDevices);
        this.logs = videoInputDevices;

        if (videoInputDevices.length === 0) {
          alert("未检测到摄像头")
          this.$router.back()
          return
        }

        // 默认获取第一个摄像头设备id
        let firstDeviceId = videoInputDevices[0].deviceId;

        // 获取第一个摄像头设备的名称
        const videoInputDeviceslablestr = JSON.stringify(videoInputDevices[0].label);
        if (videoInputDevices.length > 1) {
          // 判断是否后置摄像头
          if (videoInputDeviceslablestr.indexOf('back') > -1) {
            firstDeviceId = videoInputDevices[0].deviceId;
          } else {
            firstDeviceId = videoInputDevices[1].deviceId;
          }
        }
        this.decodeFromInputVideoFunc(firstDeviceId);

      }).catch(err => {
        this.tipShow = true;
        this.logs = err;
        console.error(err);
      });
    },
    // 重置读取器后启动持续条码识别；识别出的非空 ISBN 由回调保存并带回来源页面。
    decodeFromInputVideoFunc(firstDeviceId) {
      this.codeReader.reset(); // 重置
      // 持续更新识别提示；收到非空条码后写入全局 ISBN，结束扫描并返回上一页。
      this.codeReader.decodeFromInputVideoDeviceContinuously(firstDeviceId, 'video', (result, err) => {
        this.tipMsg = '正在尝试识别...';
        this.scanText = '';

        if (result) {
          console.log('扫描结果', result);
          this.scanText = result.text;
          if (this.scanText) {
            this.tipShow = false;
            this.$store.commit('SET_ISBN', result.text);
            this.return_back()
          }
        }

        if (err && !(err)) {
          this.tipMsg = '识别失败';
          setTimeout(() => {
            this.tipShow = false;
          }, 2000)
          console.error(err);
          this.return_back()
        }

      });
    },
    return_back() {  // 返回上一页
      // 停止摄像头占用
      this.codeReader.stopContinuousDecode()
      this.codeReader.reset();
      this.$router.back();
    }
  }
}
</script>

<style scoped>

.scan-video {
  height: 80vh;
}

.page-scan {
  overflow-y: hidden;
}
</style>
