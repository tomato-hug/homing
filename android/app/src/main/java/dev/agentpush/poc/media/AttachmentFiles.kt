package dev.agentpush.poc.media

import android.content.ClipData
import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.media.MediaScannerConnection
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.MediaStore
import androidx.core.content.FileProvider
import dev.agentpush.poc.AgentApi
import dev.agentpush.poc.AppSettings
import dev.agentpush.poc.Attachment
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File
import java.net.HttpURLConnection
import java.net.URL

object AttachmentFiles {
    suspend fun download(
        context: Context,
        settings: AppSettings,
        attachment: Attachment,
    ): File = withContext(Dispatchers.IO) {
        val safeName = attachment.name
            .replace(Regex("[^\\p{L}\\p{N}._-]+"), "_")
            .ifBlank { "attachment.bin" }
        val dir = File(context.cacheDir, "attachments").apply { mkdirs() }
        val target = File(dir, "${attachment.id.take(16)}-$safeName")
        if (target.exists() && (attachment.size <= 0 || target.length() == attachment.size)) {
            return@withContext target
        }
        val bytes = AgentApi.downloadAttachmentBytes(settings, attachment)
        val temp = File(dir, "${target.name}.part")
        temp.writeBytes(bytes)
        if (target.exists()) target.delete()
        if (!temp.renameTo(target)) {
            temp.copyTo(target, overwrite = true)
            temp.delete()
        }
        target
    }

    fun fileUri(context: Context, file: File): Uri {
        return FileProvider.getUriForFile(context, "${context.packageName}.files", file)
    }

    fun open(context: Context, file: File, mime: String) {
        val uri = fileUri(context, file)
        val intent = Intent(Intent.ACTION_VIEW).apply {
            setDataAndType(uri, mime.ifBlank { "*/*" })
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
            clipData = ClipData.newRawUri(file.name, uri)
        }
        context.startActivity(
            Intent.createChooser(intent, "用其他应用打开").addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
        )
    }

    fun share(context: Context, file: File, mime: String) {
        val uri = fileUri(context, file)
        val intent = Intent(Intent.ACTION_SEND).apply {
            type = mime.ifBlank { "*/*" }
            putExtra(Intent.EXTRA_STREAM, uri)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
            clipData = ClipData.newRawUri(file.name, uri)
        }
        context.startActivity(
            Intent.createChooser(intent, "分享文件").addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
        )
    }

    suspend fun saveImage(
        context: Context,
        file: File,
        displayName: String,
    ): Uri = withContext(Dispatchers.IO) {
        val mime = when (file.extension.lowercase()) {
            "png" -> "image/png"
            "gif" -> "image/gif"
            "webp" -> "image/webp"
            else -> "image/jpeg"
        }
        val extension = when (mime) {
            "image/png" -> "png"
            "image/gif" -> "gif"
            "image/webp" -> "webp"
            else -> "jpg"
        }
        val name = displayName.substringBeforeLast('.').ifBlank { "agent-image" }
        if (Build.VERSION.SDK_INT >= 29) {
            val values = ContentValues().apply {
                put(MediaStore.Images.Media.DISPLAY_NAME, "$name.$extension")
                put(MediaStore.Images.Media.MIME_TYPE, mime)
                put(
                    MediaStore.Images.Media.RELATIVE_PATH,
                    "${Environment.DIRECTORY_PICTURES}/Homing",
                )
                put(MediaStore.Images.Media.IS_PENDING, 1)
            }
            val resolver = context.contentResolver
            val uri = resolver.insert(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, values)
                ?: throw java.io.IOException("无法创建相册文件")
            resolver.openOutputStream(uri)?.use { output ->
                file.inputStream().use { input -> input.copyTo(output) }
            } ?: throw java.io.IOException("无法写入相册")
            values.clear()
            values.put(MediaStore.Images.Media.IS_PENDING, 0)
            resolver.update(uri, values, null, null)
            uri
        } else {
            @Suppress("DEPRECATION")
            val pictures = Environment.getExternalStoragePublicDirectory(
                Environment.DIRECTORY_PICTURES,
            )
            val dir = File(pictures, "Homing").apply { mkdirs() }
            val target = File(dir, "$name.$extension")
            file.copyTo(target, overwrite = true)
            MediaScannerConnection.scanFile(
                context,
                arrayOf(target.absolutePath),
                arrayOf(mime),
                null,
            )
            Uri.fromFile(target)
        }
    }
}
