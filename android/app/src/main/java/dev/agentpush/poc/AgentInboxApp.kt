package dev.agentpush.poc

import android.app.Application
import coil3.ImageLoader
import coil3.SingletonImageLoader
import coil3.network.okhttp.OkHttpNetworkFetcherFactory
import coil3.request.crossfade
import dev.agentpush.poc.ui.LanguagePreferences
import kotlinx.coroutines.runBlocking
import okhttp3.MediaType.Companion.toMediaTypeOrNull
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody

class AgentInboxApp : Application() {
    override fun onCreate() {
        super.onCreate()
        LanguagePreferences.load(this)
        CrashReporter.install(this)
        HomingFirebase.initialize(this)
        val coilClient = OkHttpClient.Builder()
            .addInterceptor { chain ->
                val request = chain.request()
                val url = request.url.toString()
                if (AgentApi.shouldRouteViaCloudTransport(url)) {
                    val bytes = runBlocking { AgentApi.downloadUrlViaCloudTransport(url) }
                    val mime = guessImageMime(url, bytes)
                    Response.Builder()
                        .request(request)
                        .protocol(Protocol.HTTP_1_1)
                        .code(200)
                        .message("OK")
                        .body(bytes.toResponseBody(mime.toMediaTypeOrNull()))
                        .build()
                } else {
                    chain.proceed(request)
                }
            }
            .build()
        SingletonImageLoader.setSafe { context ->
            ImageLoader.Builder(context)
                .components {
                    add(OkHttpNetworkFetcherFactory(callFactory = { coilClient }))
                }
                .crossfade(true)
                .build()
        }
    }

    private fun guessImageMime(url: String, bytes: ByteArray): String {
        if (bytes.size >= 4) {
            if (bytes[0] == 0x89.toByte() && bytes[1] == 0x50.toByte()) return "image/png"
            if (bytes[0] == 0xFF.toByte() && bytes[1] == 0xD8.toByte()) return "image/jpeg"
            if (bytes[0] == 0x47.toByte() && bytes[1] == 0x49.toByte()) return "image/gif"
            if (bytes[0] == 0x52.toByte() && bytes[1] == 0x49.toByte()) return "image/webp"
        }
        val lower = url.lowercase()
        return when {
            ".png" in lower -> "image/png"
            ".gif" in lower -> "image/gif"
            ".webp" in lower -> "image/webp"
            ".svg" in lower -> "image/svg+xml"
            else -> "image/jpeg"
        }
    }
}
