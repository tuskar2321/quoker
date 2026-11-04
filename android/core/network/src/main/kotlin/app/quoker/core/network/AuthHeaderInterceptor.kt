package app.quoker.core.network

import app.quoker.core.common.AppInfo
import javax.inject.Inject
import javax.inject.Singleton
import okhttp3.Interceptor
import okhttp3.Response

/**
 * Adds `Authorization`, `X-Quoker-Version-Code` and `X-Device-Id` to requests for the API
 * origin. Also meant for Coil's OkHttp fetcher (thumbnails need the session) and for file
 * downloads; requests to other hosts pass untouched so the token never leaks.
 */
@Singleton
class AuthHeaderInterceptor @Inject constructor(
    private val appInfo: AppInfo,
    private val urls: ApiUrls,
    private val tokens: SessionTokenProvider,
    private val devices: DeviceIdProvider,
) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        if (!urls.isApiOrigin(request.url)) return chain.proceed(request)
        val builder = request.newBuilder()
            .header(HEADER_VERSION_CODE, appInfo.versionCode.toString())
            .header(HEADER_DEVICE_ID, devices.deviceId())
        if (request.header(HEADER_AUTHORIZATION) == null) {
            tokens.sessionToken()?.let { builder.header(HEADER_AUTHORIZATION, "Bearer $it") }
        }
        return chain.proceed(builder.build())
    }

    companion object {
        const val HEADER_AUTHORIZATION = "Authorization"
        const val HEADER_VERSION_CODE = "X-Quoker-Version-Code"
        const val HEADER_DEVICE_ID = "X-Device-Id"
    }
}
