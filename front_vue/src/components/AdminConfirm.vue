<template>
  <el-dialog :model-value="modelValue" :title="title" width="540px" :close-on-click-modal="false" :close-on-press-escape="!loading" @update:model-value="$emit('update:modelValue', $event)" @closed="clear">
    <p class="danger-note">{{ description }}</p>
    <el-alert v-if="error" type="error" :title="error" :closable="false" class="form-message" />
    <el-form label-position="top" @submit.prevent="submit">
      <slot />
      <el-form-item label="操作原因"><el-input v-model="reason" type="textarea" :rows="2" maxlength="500" show-word-limit placeholder="填写原因，便于追溯" :disabled="loading" /></el-form-item>
      <el-form-item v-if="confirmName" :label="`输入用户名 ${confirmName} 确认`"><el-input v-model="typedName" autocomplete="off" :disabled="loading" /></el-form-item>
      <el-form-item label="你的当前密码"><el-input v-model="password" type="password" show-password autocomplete="current-password" placeholder="再次验证管理员身份" :disabled="loading" /></el-form-item>
      <div class="action-row"><el-button :disabled="loading" @click="$emit('update:modelValue', false)">取消</el-button><el-button type="danger" native-type="submit" :loading="loading">确认{{ title }}</el-button></div>
    </el-form>
  </el-dialog>
</template>
<script>
export default {
  name: 'AdminConfirm',
  props: {modelValue: Boolean, title: String, description: String, confirmName: String, loading: Boolean, serverError: String},
  emits: ['update:modelValue', 'confirm'],
  data() { return {reason: '', password: '', typedName: '', localError: ''} },
  computed: {error() { return this.localError || this.serverError }},
  beforeUnmount() { this.clear() },
  methods: {
    clear() { this.reason = ''; this.password = ''; this.typedName = ''; this.localError = '' },
    submit() {
      if (this.loading) return
      this.localError = !this.reason.trim() ? '请填写操作原因' : new TextEncoder().encode(this.reason.trim()).length > 512 ? '操作原因不能超过 512 个 UTF-8 字节' : !this.password ? '请输入你的当前密码' : this.confirmName && this.typedName !== this.confirmName ? '用户名确认不一致' : ''
      if (this.localError) return
      this.$emit('confirm', {reason: this.reason.trim(), current_password: this.password})
    },
  },
}
</script>
