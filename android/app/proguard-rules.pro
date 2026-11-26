# Homing Android Release R8 / ProGuard Rules

# Lazysodium & JNA native bindings (required for X25519 / Ed25519 / XChaCha20-Poly1305)
-keep class com.sun.jna.** { *; }
-keep class * implements com.sun.jna.** { *; }
-keep class com.goterl.lazysodium.** { *; }
-dontwarn com.sun.jna.**
-dontwarn com.goterl.lazysodium.**

# Room database & generated implementations
-keep class * extends androidx.room.RoomDatabase
-keep @androidx.room.Entity class * { *; }
-keep @androidx.room.Dao class * { *; }

# OkHttp & Okio
-dontwarn okhttp3.**
-dontwarn okio.**
-dontwarn javax.annotation.**
-dontwarn org.conscrypt.**
-dontwarn org.openjsse.**

# Coil 3
-keep class coil3.** { *; }

# Firebase Messaging
-keep class dev.agentpush.poc.HomingFirebaseMessagingService { *; }
-keep class dev.agentpush.poc.ReplyActionReceiver { *; }
-keep class dev.agentpush.poc.PushService { *; }

# ZXing barcode scanner
-keep class com.journeyapps.barcodescanner.** { *; }
-keep class com.google.zxing.** { *; }

# Preserve source file names & line numbers for crash diagnostics
-keepattributes SourceFile,LineNumberTable
