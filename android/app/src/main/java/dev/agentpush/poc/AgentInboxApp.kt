package dev.agentpush.poc

import android.app.Application
import coil3.ImageLoader
import coil3.SingletonImageLoader
import coil3.network.okhttp.OkHttpNetworkFetcherFactory
import coil3.request.crossfade

class AgentInboxApp : Application() {
    override fun onCreate() {
        super.onCreate()
        CrashReporter.install(this)
        SingletonImageLoader.setSafe { context ->
            ImageLoader.Builder(context)
                .components {
                    add(OkHttpNetworkFetcherFactory())
                }
                .crossfade(true)
                .build()
        }
    }
}
