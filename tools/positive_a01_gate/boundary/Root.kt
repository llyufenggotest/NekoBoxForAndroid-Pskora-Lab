package io.nekohasekai.sagernet
object Action {const val CLOSE="close"; const val RESET_UPSTREAM_CONNECTIONS="reset"}
object R {object string {const val speed_detail=1;const val speed=2;const val traffic=3;const val forward_success=4;const val stop=5;const val action_switch=6;const val reset_connections=7};object drawable {const val ic_service_active=1};object attr {const val colorPrimary=1}}
object SagerNet {val application=android.content.Context();fun configureIntent(c:android.content.Context)=Any()}
