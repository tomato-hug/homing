plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
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

android {
    namespace = "dev.agentpush.poc"
    compileSdk = 35

    defaultConfig {
        applicationId = "dev.agentpush.poc"
        minSdk = 26
        targetSdk = 35
        versionCode = 9
        versionName = "0.7.0"
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
            isMinifyEnabled = false
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
    implementation("com.mikepenz:multiplatform-markdown-renderer-m3:0.38.1")
    implementation("com.mikepenz:multiplatform-markdown-renderer-coil3:0.38.1")

    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.10.2")

    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20240303")
    testImplementation(platform("androidx.compose:compose-bom:2025.09.01"))
    testImplementation("androidx.compose.ui:ui-test-junit4")
    testImplementation("androidx.test:core:1.6.1")
    testImplementation("androidx.test.ext:junit:1.2.1")
    testImplementation("org.robolectric:robolectric:4.14.1")
    debugImplementation("androidx.compose.ui:ui-test-manifest")
}
