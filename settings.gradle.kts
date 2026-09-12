pluginManagement {
    repositories {
        gradlePluginPortal()
        google()
        mavenCentral()
        maven(url = "https://repo.maven.apache.org/maven2")
        maven(url = "https://repo1.maven.org/maven2")
        maven(url = "https://plugins.gradle.org/m2")
        maven(url = "https://maven.aliyun.com/repository/gradle-plugin")
        maven(url = "https://maven.aliyun.com/repository/google")
        maven(url = "https://maven.aliyun.com/repository/central")
    }
}

include(":app")
rootProject.name = "NB4A"
