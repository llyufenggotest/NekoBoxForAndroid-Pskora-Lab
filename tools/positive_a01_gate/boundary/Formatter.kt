package android.text.format
object Formatter {var calls=0; fun formatFileSize(c:android.content.Context,v:Long):String {calls++; return "$v B"} }
