import router from "../router/index.js"

export function handleError(resp) {
    if (resp.code === 0) {
        return null
    }
    switch (resp.code) {
        case 401:
        case 402:
        case 403:
        case 404:
            alert(resp)
            router.push('/')
    }
}