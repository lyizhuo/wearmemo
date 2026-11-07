plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.plugin.compose")
}

android {
    namespace = "dev.wearmemo"
    compileSdk = 36
    buildToolsVersion = "36.1.0"

    defaultConfig {
        applicationId = "dev.wearmemo"
        minSdk = 30
        targetSdk = 36
        versionCode = 1
        versionName = "0.1.0"

        // Where the relay lives. Override at build time until the pairing flow
        // exists:
        //   ./gradlew :app:assembleDebug -PrelayBaseUrl=http://192.168.1.20:8080
        // Default is the emulator's alias for the host machine.
        val relayBaseUrl = (project.findProperty("relayBaseUrl") as String?)
            ?: "http://10.0.2.2:8080"
        buildConfigField("String", "RELAY_BASE_URL", "\"$relayBaseUrl\"")
        buildConfigField("String", "DEVICE_TOKEN", "\"dev-token-change-me\"")
    }

    buildTypes {
        release {
            isMinifyEnabled = false
        }
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    implementation(platform("androidx.compose:compose-bom:2026.06.01"))
    implementation("androidx.activity:activity-compose:1.9.3")
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-graphics")
    implementation("androidx.compose.foundation:foundation")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.8.7")
    implementation("androidx.lifecycle:lifecycle-viewmodel-ktx:2.8.7")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.8.7")
    implementation("androidx.core:core-ktx:1.13.1")
}
