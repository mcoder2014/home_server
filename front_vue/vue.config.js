const {defineConfig} = require('@vue/cli-service')
module.exports = defineConfig({
    transpileDependencies: true,
    publicPath: '/',
    lintOnSave: false,
    // 必须用 https 才能打开手机的摄像头
    devServer:{
        // https: true
    },
})
