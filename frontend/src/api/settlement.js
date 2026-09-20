import request from './request'

// 维修费用结算接口。
export const settlementApi = {
  list: (params) => request.get('/settlements', { params }),
  detail: (id) => request.get(`/settlements/${id}`),
  create: (data) => request.post('/settlements', data),
  remove: (id) => request.delete(`/settlements/${id}`),
  submit: (id, data) => request.post(`/settlements/${id}/submit`, data),
  reject: (id, data) => request.post(`/settlements/${id}/reject`, data),
  resubmit: (id, data) => request.post(`/settlements/${id}/resubmit`, data),
  approve: (id, data) => request.post(`/settlements/${id}/approve`, data),
  diff: (id, params) => request.get(`/settlements/${id}/diff`, { params }),
  ledger: (params) => request.get('/settlements/ledger', { params }),
  preview: (params) => request.get('/settlements/preview', { params }),
  meta: () => request.get('/settlements/meta'),
}
