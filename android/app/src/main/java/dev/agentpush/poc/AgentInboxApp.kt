package dev.agentpush.poc

import android.app.Application
import coil3.ImageLoader
import coil3.SingletonImageLoader
import coil3.disk.DiskCache
import coil3.memory.MemoryCache
import coil3.network.okhttp.OkHttpNetworkFetcherFactory
import coil3.request.crossfade
import dev.agentpush.poc.ui.LanguagePreferences
import okhttp3.OkHttpClient
import okio.Path.Companion.toOkioPath
import java.util.concurrent.TimeUnit

class AgentInboxApp : Application() {
    override fun onCreate() {
        super.onCreate()
        LanguagePreferences.load(this)
        CrashReporter.install(this)
        HomingFirebase.initialize(this)
        val coilClient = OkHttpClient.Builder()
            .connectTimeout(10, TimeUnit.SECONDS)
            .readTimeout(30, TimeUnit.SECONDS)
            .addInterceptor { chain ->
                chain.proceed(AgentApi.maybeAttachLanAuth(chain.request()))
            }
            .build()
        SingletonImageLoader.setSafe { context ->
            ImageLoader.Builder(context)
                .memoryCache {
                    MemoryCache.Builder()
                        .maxSizePercent(context, 0.25)
                        .build()
                }
                .diskCache {
                    DiskCache.Builder()
                        .directory(context.cacheDir.resolve("image_cache").toOkioPath())
                        .maxSizeBytes(100L * 1024 * 1024)
                        .build()
                }
                .components {
                    add(coil3.svg.SvgDecoder.Factory())
                    add(HomingMediaFetcher.Factory())
                    add(OkHttpNetworkFetcherFactory(callFactory = { coilClient }))
                }
                .crossfade(true)
                .build()
        }
    }
}
