package dev.agentpush.poc.media

import android.content.Context
import android.net.Uri
import android.provider.OpenableColumns
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File
import java.util.UUID

data class PendingAttachment(
    val id: String = UUID.randomUUID().toString(),
    val file: File,
    val name: String,
    val mime: String,
    val size: Long,
    val progress: Float = 0f,
    val uploading: Boolean = false,
    val uploadedPath: String? = null,
    val error: String? = null,
) {
    val ready: Boolean
        get() = uploadedPath != null && !uploading && error == null
}

object PendingFiles {
    suspend fun fromUri(context: Context, uri: Uri): PendingAttachment =
        withContext(Dispatchers.IO) {
            val resolver = context.contentResolver
            var name = "附件"
            var size = 0L
            resolver.query(uri, null, null, null, null)?.use { cursor ->
                if (cursor.moveToFirst()) {
                    val nameIndex = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                    val sizeIndex = cursor.getColumnIndex(OpenableColumns.SIZE)
                    if (nameIndex >= 0) name = cursor.getString(nameIndex) ?: name
                    if (sizeIndex >= 0 && !cursor.isNull(sizeIndex)) {
                        size = cursor.getLong(sizeIndex)
                    }
                }
            }
            val mime = resolver.getType(uri) ?: mimeFromName(name)
            val safeName = name.replace(Regex("[^\\p{L}\\p{N}._-]+"), "_")
                .ifBlank { "attachment.bin" }
            val dir = File(context.cacheDir, "pending").apply { mkdirs() }
            val target = File(dir, "${System.currentTimeMillis()}-$safeName")
            resolver.openInputStream(uri)?.use { input ->
                target.outputStream().use { output -> input.copyTo(output) }
            } ?: throw java.io.IOException("无法读取所选文件")
            PendingAttachment(
                file = target,
                name = safeName,
                mime = mime,
                size = target.length().takeIf { it > 0 } ?: size,
            )
        }

    fun newCameraFile(context: Context): File {
        val dir = File(context.cacheDir, "camera").apply { mkdirs() }
        return File(dir, "photo-${System.currentTimeMillis()}.jpg")
    }

    fun cameraAttachment(file: File): PendingAttachment {
        return PendingAttachment(
            file = file,
            name = file.name,
            mime = "image/jpeg",
            size = file.length(),
        )
    }

    fun mimeFromName(name: String): String {
        return when (name.substringAfterLast('.', "").lowercase()) {
            "jpg", "jpeg" -> "image/jpeg"
            "png" -> "image/png"
            "gif" -> "image/gif"
            "webp" -> "image/webp"
            "pdf" -> "application/pdf"
            "html", "htm" -> "text/html"
            "json" -> "application/json"
            "txt", "md", "log" -> "text/plain"
            "zip" -> "application/zip"
            else -> "application/octet-stream"
        }
    }
}
