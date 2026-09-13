import axios from 'axios'
const {browserHeaders, requestError} = require('@/api/browser_client.cjs')
const client = axios.create({baseURL: '/', withCredentials: true})
client.interceptors.request.use(config => {
    config.headers = browserHeaders(config.headers)
    return config
})
client.interceptors.response.use(response => response, error => Promise.reject(requestError(error, error.config?.headers?.['X-CSRF-Token'] || '')))
export default client
