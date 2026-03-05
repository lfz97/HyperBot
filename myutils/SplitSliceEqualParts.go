package myutils

//切分slice为等份
func SplitSliceEqualParts[T any](slice []T,parts int) [][]T{
	if parts<=0{
		return [][]T{slice}
	}
	length:=len(slice)
	if length <=parts {
		return [][]T{slice}
	}
	quotient:=length/parts
	
	result:=[][]T{}

	//定义一个游标记录指向的slice下标位置
	cursor:=0
	//定义一个计数器记录循环了几次
	loop_num:=0

	//定义一个临时游标记录指向的slice下标位置，每次取[c , c+quotient]范围的slice,然后将cursor 和 c 都移动到c+quotient位置	
	for c:=0;c<length;c+=quotient{
		//循环计数器每次循环+1，记录循环次数，由于循环计数器是从0开始计数的，所以当循环计数器等于parts时，说明已经分割出parts-1份，跳出循环
		loop_num+=1
		if loop_num==parts{
			break
		}
		//取[c , c+quotient]范围的slice，追加到result中
		temp_s:=slice[c:c+quotient]
		result=append(result, temp_s)
		//cursor也移动到c+quotient位置
		cursor=c+quotient
		
	}
	//最后一份直接追加剩余的slice
	result=append(result, slice[cursor:])
	return result
}