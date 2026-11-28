package dev.agentpush.poc

import coil3.ImageLoader
import coil3.Uri
import coil3.decode.DataSource
import coil3.decode.ImageSource
import coil3.fetch.FetchResult
import coil3.fetch.Fetcher
import coil3.fetch.SourceFetchResult
import coil3.request.Options
import okio.Buffer
import okio.FileSystem
import java.io.IOException

/**
 * Custom Coil 3 [Fetcher] for Homing capability (`/f/...`) and project raw (`/api/fs/raw`)
 * URLs. Suspends natively over [AgentApi.downloadMediaBytes] (routing transparently between
 * authenticated LAN HTTP and chunked E2EE cloud RPC with automatic fallback) and persists
 * decrypted bytes into Coil's [coil3.disk.DiskCache] under a route-independent key.
 */
class HomingMediaFetcher(
    private val url: String,
    private val options: Options,
    private val imageLoader: ImageLoader,
) : Fetcher {

    override suspend fun fetch(): FetchResult {
        val cacheKey = options.diskCacheKey ?: AgentApi.mediaCacheKey(url)
        val diskCache = imageLoader.diskCache

        if (options.diskCachePolicy.readEnabled && diskCache != null) {
            val snapshot = diskCache.openSnapshot(cacheKey)
            if (snapshot != null) {
                return try {
                    val header = diskCache.fileSystem.read(snapshot.data) {
                        val size = minOf(buffer.size, 256L).toInt()
                        if (size > 0) readByteArray(size.toLong()) else readByteArray()
                    }
                    if (header.isEmpty()) {
                        snapshot.close()
                        throw IOException("缓存图片为空")
                    }
                    SourceFetchResult(
                        source = ImageSource(
                            file = snapshot.data,
                            fileSystem = diskCache.fileSystem,
                            diskCacheKey = cacheKey,
                            closeable = snapshot,
                        ),
                        mimeType = guessImageMime(url, header),
                        dataSource = DataSource.DISK,
                    )
                } catch (t: Throwable) {
                    runCatching { snapshot.close() }
                    if (t is IOException && t.message == "缓存图片为空") {
                        diskCache.remove(cacheKey)
                        fetchRemote(cacheKey, diskCache)
                    } else {
                        throw t
                    }
                }
            }
        }

        return fetchRemote(cacheKey, diskCache)
    }

    private suspend fun fetchRemote(
        cacheKey: String,
        diskCache: coil3.disk.DiskCache?,
    ): FetchResult {
        val bytes = AgentApi.downloadMediaBytes(url)
        if (bytes.isEmpty()) {
            throw IOException("图片内容为空")
        }
        val mime = guessImageMime(url, bytes)
        if (options.diskCachePolicy.writeEnabled && diskCache != null) {
            val editor = diskCache.openEditor(cacheKey)
            if (editor != null) {
                try {
                    diskCache.fileSystem.write(editor.data) {
                        write(bytes)
                    }
                    val snapshot = editor.commitAndOpenSnapshot()
                    if (snapshot != null) {
                        return SourceFetchResult(
                            source = ImageSource(
                                file = snapshot.data,
                                fileSystem = diskCache.fileSystem,
                                diskCacheKey = cacheKey,
                                closeable = snapshot,
                            ),
                            mimeType = mime,
                            dataSource = DataSource.NETWORK,
                        )
                    }
                } catch (t: Throwable) {
                    runCatching { editor.abort() }
                }
            }
        }
        return SourceFetchResult(
            source = ImageSource(
                source = Buffer().write(bytes),
                fileSystem = FileSystem.SYSTEM,
            ),
            mimeType = mime,
            dataSource = DataSource.NETWORK,
        )
    }

    class Factory : Fetcher.Factory<Uri> {
        override fun create(
            data: Uri,
            options: Options,
            imageLoader: ImageLoader,
        ): Fetcher? {
            val url = data.toString()
            if (!AgentApi.isHomingMediaUrl(url)) return null
            return HomingMediaFetcher(url, options, imageLoader)
        }
    }
}

internal fun guessImageMime(url: String, bytes: ByteArray): String {
    if (bytes.size >= 4) {
        if (bytes[0] == 0x89.toByte() && bytes[1] == 0x50.toByte()) return "image/png"
        if (bytes[0] == 0xFF.toByte() && bytes[1] == 0xD8.toByte()) return "image/jpeg"
        if (bytes[0] == 0x47.toByte() && bytes[1] == 0x49.toByte()) return "image/gif"
        if (bytes[0] == 0x52.toByte() && bytes[1] == 0x49.toByte()) return "image/webp"
        if (bytes[0] == 0x42.toByte() && bytes[1] == 0x4D.toByte()) return "image/bmp"
        if (bytes.size >= 12 &&
            bytes[4] == 0x66.toByte() &&
            bytes[5] == 0x74.toByte() &&
            bytes[6] == 0x79.toByte() &&
            bytes[7] == 0x70.toByte()
        ) {
            return "image/avif"
        }
        val head = String(bytes, 0, minOf(bytes.size, 256), Charsets.UTF_8).trimStart().lowercase()
        if (head.startsWith("<svg") || (head.startsWith("<?xml") && "<svg" in head)) {
            return "image/svg+xml"
        }
    }
    val lower = url.lowercase()
    return when {
        ".svg" in lower -> "image/svg+xml"
        ".png" in lower -> "image/png"
        ".gif" in lower -> "image/gif"
        ".webp" in lower -> "image/webp"
        ".bmp" in lower -> "image/bmp"
        ".avif" in lower -> "image/avif"
        else -> "image/jpeg"
    }
}
