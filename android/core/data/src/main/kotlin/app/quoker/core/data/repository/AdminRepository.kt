package app.quoker.core.data.repository

import app.quoker.core.data.model.AdminStatus
import app.quoker.core.data.model.AdminUser
import app.quoker.core.data.model.CreatedInvite
import app.quoker.core.data.model.Invite
import app.quoker.core.data.model.Role
import app.quoker.core.data.model.Worker
import app.quoker.core.data.model.toDomain
import app.quoker.core.data.model.wire
import app.quoker.core.network.QuokerApi
import app.quoker.core.network.model.AllowWorkerRequestDto
import app.quoker.core.network.model.CreateInviteRequestDto
import app.quoker.core.network.quokerCall
import app.quoker.core.network.textPlain
import javax.inject.Inject

/** Admin endpoints; every call fails with `forbidden` for a non-admin. */
interface AdminRepository {
    suspend fun status(): AdminStatus

    /** Not revoked, newest first. */
    suspend fun invites(): List<Invite>

    /** [expiresInDays] 1..30. The returned code is shown once. */
    suspend fun createInvite(role: Role = Role.USER, expiresInDays: Int = DEFAULT_INVITE_DAYS): CreatedInvite

    suspend fun revokeInvite(inviteId: Long)

    suspend fun users(): List<AdminUser>

    /** Ends all the user's sessions; blocking yourself fails with `forbidden`. */
    suspend fun blockUser(userId: Long)

    suspend fun unblockUser(userId: Long)

    suspend fun workers(): List<Worker>

    /** [fingerprint]: hex SHA-256 of the worker's client certificate. */
    suspend fun allowWorker(workerId: String, fingerprint: String)

    suspend fun revokeWorker(workerId: String)

    /** Netscape cookies.txt content. */
    suspend fun uploadInstagramCookies(cookiesTxt: String)

    companion object {
        const val DEFAULT_INVITE_DAYS = 7
        const val MAX_INVITE_DAYS = 30
    }
}

internal class AdminRepositoryImpl @Inject constructor(
    private val api: QuokerApi,
) : AdminRepository {
    override suspend fun status(): AdminStatus = quokerCall { api.adminStatus() }.toDomain()

    override suspend fun invites(): List<Invite> = quokerCall { api.invites() }.items.map { it.toDomain() }

    override suspend fun createInvite(role: Role, expiresInDays: Int): CreatedInvite =
        quokerCall { api.createInvite(CreateInviteRequestDto(role.wire(), expiresInDays)) }.toDomain()

    override suspend fun revokeInvite(inviteId: Long) = quokerCall { api.revokeInvite(inviteId) }

    override suspend fun users(): List<AdminUser> = quokerCall { api.users() }.items.map { it.toDomain() }

    override suspend fun blockUser(userId: Long) = quokerCall { api.blockUser(userId) }

    override suspend fun unblockUser(userId: Long) = quokerCall { api.unblockUser(userId) }

    override suspend fun workers(): List<Worker> = quokerCall { api.workers() }.items.map { it.toDomain() }

    override suspend fun allowWorker(workerId: String, fingerprint: String) =
        quokerCall { api.allowWorker(AllowWorkerRequestDto(workerId.trim(), fingerprint.trim())) }

    override suspend fun revokeWorker(workerId: String) = quokerCall { api.revokeWorker(workerId) }

    override suspend fun uploadInstagramCookies(cookiesTxt: String) =
        quokerCall { api.putInstagramCookies(textPlain(cookiesTxt)) }
}
