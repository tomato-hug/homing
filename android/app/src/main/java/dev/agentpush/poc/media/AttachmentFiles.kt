package dev.agentpush.poc.media

import android.content.ActivityNotFoundException
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
import java.io.IOException

object AttachmentFiles {
    suspend fun download(
        context: Context,
        settings: AppSettings,
        attachment: Attachment,
    ): File = withContext(Dispatchers.IO) {
        val safeId = attachment.id
            .replace(Regex("[^\\p{L}\\p{N}._-]+"), "_")
            .take(16)
            .ifBlank { "att" }
        val safeName = attachment.name
            .replace(Regex("[^\\p{L}\\p{N}._-]+"), "_")
            .ifBlank { "attachment.bin" }
        val dir = File(context.cacheDir, "attachments").apply { mkdirs() }
        val target = File(dir, "$safeId-$safeName")
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
        val chooser = Intent.createChooser(intent, "用其他应用打开").apply {
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_GRANT_READ_URI_PERMISSION)
            clipData = ClipData.newRawUri(file.name, uri)
        }
        try {
            context.startActivity(chooser)
        } catch (_: ActivityNotFoundException) {
            throw IOException("手机上没有可打开此类型文件的应用")
        }
    }

    fun share(context: Context, file: File, mime: String) {
        val uri = fileUri(context, file)
        val intent = Intent(Intent.ACTION_SEND).apply {
            type = mime.ifBlank { "*/*" }
            putExtra(Intent.EXTRA_STREAM, uri)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
            clipData = ClipData.newRawUri(file.name, uri)
        }
        val chooser = Intent.createChooser(intent, "分享文件").apply {
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_GRANT_READ_URI_PERMISSION)
            clipData = ClipData.newRawUri(file.name, uri)
        }
        try {
            context.startActivity(chooser)
        } catch (_: ActivityNotFoundException) {
            throw IOException("手机上没有可接收分享的应用")
        }
    }

    suspend fun saveImage(
        context: Context,
        file: File,
        displayName: String,
    ): Uri = withContext(Dispatchers.IO) {
        val ext = file.extension.lowercase().ifBlank {
            displayName.substringAfterLast('.', "").lowercase()
        }
        val mime = when (ext) {
            "png" -> "image/png"
            "gif" -> "image/gif"
            "webp" -> "image/webp"
            "svg" -> "image/svg+xml"
            else -> "image/jpeg"
        }
        val extension = when (mime) {
            "image/png" -> "png"
            "image/gif" -> "gif"
            "image/webp" -> "webp"
            "image/svg+xml" -> "svg"
            else -> "jpg"
        }
        val name = displayName.substringBeforeLast('.').ifBlank { "agent-image" }
        if (Build.VERSION.SDK_INT >= 29) {
            val resolver = context.contentResolver
            if (mime == "image/svg+xml") {
                val values = ContentValues().apply {
                    put(MediaStore.Downloads.DISPLAY_NAME, "$name.$extension")
                    put(MediaStore.Downloads.MIME_TYPE, mime)
                    put(MediaStore.Downloads.RELATIVE_PATH, "${Environment.DIRECTORY_DOWNLOADS}/Homing")
                    put(MediaStore.Downloads.IS_PENDING, 1)
                }
                val uri = resolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values)
                    ?: throw IOException("无法创建下载文件")
                resolver.openOutputStream(uri)?.use { output ->
                    file.inputStream().use { input -> input.copyTo(output) }
                } ?: throw IOException("无法写入文件")
                values.clear()
                values.put(MediaStore.Downloads.IS_PENDING, 0)
                resolver.update(uri, values, null, null)
                return@withContext uri
            }
            val values = ContentValues().apply {
                put(MediaStore.Images.Media.DISPLAY_NAME, "$name.$extension")
                put(MediaStore.Images.Media.MIME_TYPE, mime)
                put(
                    MediaStore.Images.Media.RELATIVE_PATH,
                    "${Environment.DIRECTORY_PICTURES}/Homing",
                )
                put(MediaStore.Images.Media.IS_PENDING, 1)
            }
            val uri = resolver.insert(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, values)
                ?: throw IOException("无法创建相册文件")
            resolver.openOutputStream(uri)?.use { output ->
                file.inputStream().use { input -> input.copyTo(output) }
            } ?: throw IOException("无法写入相册")
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
