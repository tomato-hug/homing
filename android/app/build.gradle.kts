plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
    id("com.google.devtools.ksp")
}

val homingReleaseStoreFile = providers.environmentVariable("HOMING_RELEASE_STORE_FILE").orNull
val homingReleaseStorePassword =
    providers.environmentVariable("HOMING_RELEASE_STORE_PASSWORD").orNull
val homingReleaseKeyAlias =
    providers.environmentVariable("HOMING_RELEASE_KEY_ALIAS").orNull ?: "homing"
val homingReleaseKeyPassword =
    providers.environmentVariable("HOMING_RELEASE_KEY_PASSWORD").orNull
val hasHomingReleaseSigning =
    !homingReleaseStoreFile.isNullOrBlank() &&
        !homingReleaseStorePassword.isNullOrBlank() &&
        !homingReleaseKeyPassword.isNullOrBlank()

fun firebaseBuildConfig(name: String): String {
    val value = providers.environmentVariable(name).orNull.orEmpty().trim()
    val escaped = value.replace("\\", "\\\\").replace("\"", "\\\"")
    return "\"$escaped\""
}

android {
    namespace = "dev.agentpush.poc"
    compileSdk = 35

    defaultConfig {
        applicationId = "dev.homing.app"
        minSdk = 26
        targetSdk = 35
        versionCode = 32
        versionName = "0.11.6-beta.13"

        val enableDirectDonation =
            providers.environmentVariable("HOMING_ENABLE_DIRECT_DONATION")
                .orNull
                ?.lowercase() != "false"
        buildConfigField(
            "boolean",
            "ENABLE_DIRECT_DONATION",
            enableDirectDonation.toString(),
        )

        buildConfigField(
            "String",
            "FIREBASE_PROJECT_ID",
            firebaseBuildConfig("HOMING_FIREBASE_PROJECT_ID"),
        )
        buildConfigField(
            "String",
            "FIREBASE_APPLICATION_ID",
            firebaseBuildConfig("HOMING_FIREBASE_APPLICATION_ID"),
        )
        buildConfigField(
            "String",
            "FIREBASE_API_KEY",
            firebaseBuildConfig("HOMING_FIREBASE_API_KEY"),
        )
        buildConfigField(
            "String",
            "FIREBASE_SENDER_ID",
            firebaseBuildConfig("HOMING_FIREBASE_SENDER_ID"),
        )
    }

    signingConfigs {
        if (hasHomingReleaseSigning) {
            create("release") {
                storeFile = file(homingReleaseStoreFile!!)
                storePassword = homingReleaseStorePassword
                keyAlias = homingReleaseKeyAlias
                keyPassword = homingReleaseKeyPassword
                storeType = "JKS"
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            signingConfig = signingConfigs.findByName("release")
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro",
            )
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    testOptions {
        unitTests.isIncludeAndroidResources = true
    }
}

tasks.register("homingRelease") {
    group = "build"
    description = "Builds a release APK signed with the Homing release key."
    dependsOn("assembleRelease")
    doLast {
        println(
            "Homing signed release APK: " +
                layout.buildDirectory
                    .file("outputs/apk/release/app-release.apk")
                    .get()
                    .asFile
                    .absolutePath,
        )
    }
}

tasks.configureEach {
    if (name == "preReleaseBuild") {
        doFirst {
            check(hasHomingReleaseSigning) {
                "Homing release signing requires HOMING_RELEASE_STORE_FILE, " +
                    "HOMING_RELEASE_STORE_PASSWORD, and HOMING_RELEASE_KEY_PASSWORD."
            }
            check(file(homingReleaseStoreFile!!).isFile) {
                "Homing release keystore not found: $homingReleaseStoreFile"
            }
        }
    }
}

dependencies {
    val composeBom = platform("androidx.compose:compose-bom:2025.09.01")
    implementation(composeBom)

    implementation("androidx.core:core-ktx:1.16.0")
    implementation("androidx.activity:activity-compose:1.10.1")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.9.1")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.9.1")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.9.1")
    implementation("androidx.navigation:navigation-compose:2.9.0")

    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-text")
    implementation("androidx.compose.ui:ui-util")
    implementation("androidx.compose.foundation:foundation")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-extended")

    implementation("io.coil-kt.coil3:coil-compose:3.3.0")
    implementation("io.coil-kt.coil3:coil-network-okhttp:3.3.0")
    implementation("io.coil-kt.coil3:coil-gif:3.3.0")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("com.google.firebase:firebase-messaging:24.1.2")
    implementation("com.mikepenz:multiplatform-markdown-renderer-m3:0.38.1")
    implementation("com.mikepenz:multiplatform-markdown-renderer-coil3:0.38.1")
    implementation("com.goterl:lazysodium-android:5.1.0@aar")
    implementation("net.java.dev.jna:jna:5.12.1@aar")
    implementation("com.journeyapps:zxing-android-embedded:4.3.0")
    implementation("com.google.zxing:core:3.5.3")
    implementation("androidx.appcompat:appcompat:1.7.1")
    implementation("androidx.room:room-runtime:2.7.2")
    implementation("androidx.room:room-ktx:2.7.2")
    ksp("androidx.room:room-compiler:2.7.2")

    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.10.2")

    testImplementation("junit:junit:4.13.2")
    testImplementation("org.jetbrains.kotlinx:kotlinx-coroutines-test:1.10.2")
    testImplementation("org.json:json:20240303")
    testImplementation(platform("androidx.compose:compose-bom:2025.09.01"))
    testImplementation("androidx.compose.ui:ui-test-junit4")
    testImplementation("androidx.test:core:1.6.1")
    testImplementation("androidx.test.ext:junit:1.2.1")
    testImplementation("org.robolectric:robolectric:4.14.1")
    testImplementation("com.goterl:lazysodium-java:5.1.0")
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
    debugImplementation("androidx.compose.ui:ui-test-manifest")
}
