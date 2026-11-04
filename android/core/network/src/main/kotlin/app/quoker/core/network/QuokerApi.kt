package app.quoker.core.network

import app.quoker.core.network.model.AdminStatusDto
import app.quoker.core.network.model.AdminUsersResponseDto
import app.quoker.core.network.model.AllowWorkerRequestDto
import app.quoker.core.network.model.AppVersionDto
import app.quoker.core.network.model.CreateInviteRequestDto
import app.quoker.core.network.model.CreateSendRequestDto
import app.quoker.core.network.model.CreatedInviteDto
import app.quoker.core.network.model.HistoryPageDto
import app.quoker.core.network.model.InvitesResponseDto
import app.quoker.core.network.model.MeDto
import app.quoker.core.network.model.PatchTargetRequestDto
import app.quoker.core.network.model.PrepareResponseDto
import app.quoker.core.network.model.RegisterRequestDto
import app.quoker.core.network.model.RegisterResponseDto
import app.quoker.core.network.model.ResolveRequestDto
import app.quoker.core.network.model.ResolveResponseDto
import app.quoker.core.network.model.SendResponseDto
import app.quoker.core.network.model.TargetResponseDto
import app.quoker.core.network.model.TargetsResponseDto
import app.quoker.core.network.model.TaskDto
import app.quoker.core.network.model.TelegramLinkDto
import app.quoker.core.network.model.TelegramStatusDto
import app.quoker.core.network.model.VideoResponseDto
import app.quoker.core.network.model.WorkersResponseDto
import okhttp3.RequestBody
import retrofit2.http.Body
import retrofit2.http.DELETE
import retrofit2.http.GET
import retrofit2.http.Header
import retrofit2.http.PATCH
import retrofit2.http.POST
import retrofit2.http.PUT
import retrofit2.http.Path
import retrofit2.http.Query

/**
 * Retrofit binding of api/openapi.yaml. Every call throws [QuokerException] on a non-2xx
 * response or a network failure (see [ErrorMappingInterceptor]). The thumbnail and file
 * endpoints are plain URLs (see [ApiUrls]) loaded by Coil / the download worker.
 */
interface QuokerApi {
    // Auth

    @POST("v1/auth/register")
    suspend fun register(@Body body: RegisterRequestDto): RegisterResponseDto

    @POST("v1/auth/logout")
    suspend fun logout()

    @GET("v1/me")
    suspend fun me(): MeDto

    @POST("v1/auth/telegram/link")
    suspend fun createTelegramLink(): TelegramLinkDto

    @GET("v1/auth/telegram/status")
    suspend fun telegramStatus(): TelegramStatusDto

    @DELETE("v1/auth/telegram")
    suspend fun unlinkTelegram()

    // Videos and tasks

    @POST("v1/videos/resolve")
    suspend fun resolve(@Body body: ResolveRequestDto): ResolveResponseDto

    @GET("v1/tasks/{task_id}")
    suspend fun task(@Path("task_id") taskId: String): TaskDto

    @GET("v1/videos/{video_id}")
    suspend fun video(@Path("video_id") videoId: String): VideoResponseDto

    @POST("v1/videos/{video_id}/prepare")
    suspend fun prepare(@Path("video_id") videoId: String): PrepareResponseDto

    @POST("v1/videos/{video_id}/downloaded")
    suspend fun markDownloaded(@Path("video_id") videoId: String)

    // Targets

    @GET("v1/targets")
    suspend fun targets(@Query("include_hidden") includeHidden: Boolean = false): TargetsResponseDto

    @PATCH("v1/targets/{target_id}")
    suspend fun patchTarget(
        @Path("target_id") targetId: Long,
        @Body body: PatchTargetRequestDto,
    ): TargetResponseDto

    @POST("v1/targets/refresh")
    suspend fun refreshTargets()

    // Sends

    @POST("v1/sends")
    suspend fun createSend(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body body: CreateSendRequestDto,
    ): SendResponseDto

    @GET("v1/sends/{send_id}")
    suspend fun send(@Path("send_id") sendId: Long): SendResponseDto

    // History

    @GET("v1/history")
    suspend fun history(
        @Query("cursor") cursor: String? = null,
        @Query("limit") limit: Int? = null,
    ): HistoryPageDto

    // App

    @GET("v1/app/version")
    suspend fun appVersion(): AppVersionDto

    // Admin

    @GET("v1/admin/status")
    suspend fun adminStatus(): AdminStatusDto

    @GET("v1/admin/invites")
    suspend fun invites(): InvitesResponseDto

    @POST("v1/admin/invites")
    suspend fun createInvite(@Body body: CreateInviteRequestDto): CreatedInviteDto

    @DELETE("v1/admin/invites/{invite_id}")
    suspend fun revokeInvite(@Path("invite_id") inviteId: Long)

    @GET("v1/admin/users")
    suspend fun users(): AdminUsersResponseDto

    @POST("v1/admin/users/{user_id}/block")
    suspend fun blockUser(@Path("user_id") userId: Long)

    @POST("v1/admin/users/{user_id}/unblock")
    suspend fun unblockUser(@Path("user_id") userId: Long)

    @GET("v1/admin/workers")
    suspend fun workers(): WorkersResponseDto

    @POST("v1/admin/workers")
    suspend fun allowWorker(@Body body: AllowWorkerRequestDto)

    @DELETE("v1/admin/workers/{worker_id}")
    suspend fun revokeWorker(@Path("worker_id") workerId: String)

    /** [cookies] is a Netscape cookies.txt as `text/plain`, see [textPlain]. */
    @PUT("v1/admin/instagram-cookies")
    suspend fun putInstagramCookies(@Body cookies: RequestBody)
}
