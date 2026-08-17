import java.util.Properties
import java.io.FileInputStream

plugins {
    id("com.android.application")
    id("kotlin-android")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

// Release signing, driven by android/key.properties (storePassword, keyPassword,
// keyAlias, storeFile). That file is gitignored — see flutter/.gitignore and
// flutter/android/.gitignore, plus AGENTS.md sections 5 and 6 — and is never
// present on a fresh clone. Loaded here, rather than failing the build, so
// `flutter build apk --release` still works with no manual setup: the
// `signingConfigs.release` block below is only wired up when the file and the
// keystore it points at both exist; otherwise the release build type falls
// back to the debug signing config, same as it did before this file existed.
val keyPropertiesFile = rootProject.file("key.properties")
val keyProperties = Properties()
val hasReleaseSigning = keyPropertiesFile.exists().also { exists ->
    if (exists) {
        FileInputStream(keyPropertiesFile).use { keyProperties.load(it) }
    }
}

android {
    namespace = "org.buetow.restforge"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = JavaVersion.VERSION_17.toString()
    }

    defaultConfig {
        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).
        applicationId = "org.buetow.restforge"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        minSdk = flutter.minSdkVersion
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    signingConfigs {
        if (hasReleaseSigning) {
            create("release") {
                storeFile = file(keyProperties.getProperty("storeFile"))
                storePassword = keyProperties.getProperty("storePassword")
                keyAlias = keyProperties.getProperty("keyAlias")
                keyPassword = keyProperties.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            // Real signing when android/key.properties exists (see the block
            // above); otherwise fall back to the debug keys so a fresh clone
            // with no keystore configured still produces an installable,
            // if unpublishable, APK. `flutter build apk --release` must never
            // hard-fail for lack of a keystore.
            signingConfig = if (hasReleaseSigning) {
                signingConfigs.getByName("release")
            } else {
                signingConfigs.getByName("debug")
            }
        }
    }
}

flutter {
    source = "../.."
}
