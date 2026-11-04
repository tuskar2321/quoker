package app.quoker.core.network

import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody

/** `text/plain` body, e.g. the Netscape cookies.txt for [QuokerApi.putInstagramCookies]. */
fun textPlain(text: String): RequestBody = text.toRequestBody("text/plain; charset=utf-8".toMediaType())
