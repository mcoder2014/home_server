<template>
  <el-dialog :model-value="modelValue" title="更换头像" width="min(520px, 96vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" @update:model-value="$emit('update:modelValue', $event)" @closed="clear">
    <p class="muted">JPEG / PNG，最多 2 MiB；宽高不超过 4096 像素，总像素不超过 1600 万。不支持动画图片。</p>
    <input ref="file" type="file" accept="image/jpeg,image/png" aria-label="选择头像图片" :disabled="saving || decoding" @change="choose" />
    <el-alert v-if="error" type="error" :title="error" :closable="false" class="form-message" />
    <p v-if="decoding" role="status">正在读取图片…</p>
    <div v-if="ready" class="crop-area">
      <p class="muted">拖动圆形预览调整位置，或聚焦预览后使用方向键移动。</p>
      <canvas ref="preview" width="280" height="280" tabindex="0" role="img" aria-label="头像裁剪预览，可拖动或使用方向键移动" @pointerdown="startDrag" @pointermove="drag" @pointerup="endDrag" @pointercancel="endDrag" @keydown="moveKey" />
      <el-form-item label="头像缩放"><el-slider v-model="zoom" :min="1" :max="3" :step="0.01" :disabled="saving" label="头像缩放" /></el-form-item>
    </div>
    <template #footer><el-button :disabled="saving" @click="$emit('update:modelValue', false)">取消</el-button><el-button v-if="conflict" :disabled="saving" @click="$emit('conflict')">查看最新资料</el-button><el-button type="primary" :loading="saving" :disabled="!ready || decoding || conflict" @click="save">保存头像</el-button></template>
  </el-dialog>
</template>
<script>
import {markRaw} from 'vue'
const {accountsApi} = require('@/api/accounts.cjs')
const {avatarFileError, avatarMetadata, avatarCrop} = require('@/utils/accounts_behavior.cjs')
export default {
  name: 'AvatarCropper',
  props: {modelValue: Boolean, revision: [String, Number], conflict: Boolean},
  emits: ['update:modelValue', 'saved', 'conflict', 'draft-change'],
  data() { return {image: null, ready: false, decoding: false, saving: false, error: '', zoom: 1, offsetX: 0, offsetY: 0, pointer: null, sequence: 0} },
  watch: {zoom() { this.draw() }, modelValue(value) { if (!value) this.sequence++ }},
  beforeUnmount() { this.clear() },
  methods: {
    clear() {
      this.sequence++
      if (this.image?.close) this.image.close()
      this.image = null; this.ready = false; this.decoding = false; this.error = ''; this.pointer = null
      if (this.$refs.file) this.$refs.file.value = ''
      this.$emit('draft-change', false)
    },
    // 文件头检查在解码之前。浏览器按 EXIF 方向解码，预览与最终裁剪共用同一图像，取消不产生上传。
    async choose(event) {
      const file = event.target.files?.[0]
      if (!file || this.saving) return
      this.clear(); const sequence = this.sequence
      this.error = avatarFileError(file)
      if (this.error) return
      this.decoding = true
      let image
      try {
        const metadata = avatarMetadata(await file.arrayBuffer())
        if (metadata.type !== file.type) throw new Error('文件真实格式与 JPEG / PNG 类型不一致')
        if (sequence !== this.sequence) return
        if (typeof createImageBitmap === 'function') image = await createImageBitmap(file, {imageOrientation: 'from-image'})
        else {
          const url = URL.createObjectURL(file)
          try { image = await new Promise((resolve, reject) => { const next = new Image(); next.onload = () => resolve(next); next.onerror = () => reject(new Error('图片无法解码，请重新选择')); next.src = url }) }
          finally { URL.revokeObjectURL(url) }
        }
        if (sequence !== this.sequence) { if (image.close) image.close(); return }
        const width = image.width || image.naturalWidth, height = image.height || image.naturalHeight
        if (!width || !height || width > 4096 || height > 4096 || width * height > 16000000) throw new Error('解码后的图片超过像素上限')
        this.image = markRaw(image); this.zoom = 1; this.offsetX = this.offsetY = 0; this.ready = true
        this.$emit('draft-change', true)
        await this.$nextTick(); this.draw()
      } catch (error) {
        if (image?.close) image.close()
        if (sequence === this.sequence) { this.error = error.message || '图片无法解码'; this.ready = false }
      } finally { if (sequence === this.sequence) this.decoding = false }
    },
    draw() {
      if (!this.image || !this.$refs.preview) return
      const crop = avatarCrop(this.image.width || this.image.naturalWidth, this.image.height || this.image.naturalHeight, this.zoom, this.offsetX, this.offsetY)
      this.offsetX = crop.offsetX; this.offsetY = crop.offsetY
      const context = this.$refs.preview.getContext('2d')
      context.clearRect(0, 0, 280, 280)
      context.drawImage(this.image, crop.x, crop.y, crop.size, crop.size, 0, 0, 280, 280)
    },
    startDrag(event) {
      if (this.saving) return
      event.preventDefault()
      this.pointer = {id: event.pointerId, x: event.clientX, y: event.clientY}
      event.target.setPointerCapture(event.pointerId)
    },
    drag(event) {
      if (!this.pointer || this.pointer.id !== event.pointerId || this.saving) return
      const scale = 280 / event.target.getBoundingClientRect().width
      this.offsetX += (event.clientX - this.pointer.x) * scale; this.offsetY += (event.clientY - this.pointer.y) * scale
      this.pointer.x = event.clientX; this.pointer.y = event.clientY
      this.draw()
    },
    endDrag() { this.pointer = null },
    moveKey(event) {
      const moves = {ArrowLeft: [-10, 0], ArrowRight: [10, 0], ArrowUp: [0, -10], ArrowDown: [0, 10]}
      if (!moves[event.key] || this.saving) return
      event.preventDefault()
      this.offsetX += moves[event.key][0]; this.offsetY += moves[event.key][1]
      this.draw()
    },
    async save() {
      if (!this.ready || this.saving || this.conflict) return
      this.saving = true; this.error = ''
      try {
        const crop = avatarCrop(this.image.width || this.image.naturalWidth, this.image.height || this.image.naturalHeight, this.zoom, this.offsetX, this.offsetY)
        const canvas = document.createElement('canvas'); canvas.width = canvas.height = 256
        canvas.getContext('2d').drawImage(this.image, crop.x, crop.y, crop.size, crop.size, 0, 0, 256, 256)
        const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'))
        if (!blob || blob.size > 2 * 1024 * 1024) throw new Error('无法生成有效头像，请重新选图')
        const form = new FormData(); form.append('file', blob, 'avatar.png')
        const me = await accountsApi.uploadAvatar(this.revision, form)
        this.$emit('saved', me); this.$emit('update:modelValue', false)
      } catch (error) {
        this.error = error.status === 409 ? '资料已变化，裁剪草稿已保留。请查看最新资料，核对后继续保存。' : error.message || '头像保存失败，请先刷新资料确认结果'
        if (error.status === 409) this.$emit('conflict')
      } finally { this.saving = false }
    },
  },
}
</script>
<style scoped>
input[type=file]{max-width:100%;min-height:44px;margin:12px 0}.crop-area{margin-top:16px}.crop-area canvas{display:block;width:min(280px,100%);height:auto;aspect-ratio:1;margin:12px auto 20px;border-radius:50%;background:repeating-conic-gradient(#f2f2f2 0% 25%,#fff 0% 50%) 0/20px 20px;touch-action:none;cursor:move;outline-offset:4px}.crop-area :deep(.el-slider){margin:12px 16px}
</style>
